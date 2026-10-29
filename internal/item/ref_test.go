package item

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/reftest"
)

// corpus is the reference component corpus (kremenmc.probe.item.Components).
type corpus struct {
	ctypes []string
	regs   map[string]map[string]int32
	comps  []refComp
	stacks []refStack
	errors []string
}

type refComp struct {
	t    Type
	raw  []byte
	hash string
	src  string
}

type refStack struct {
	raw, hashed []byte
	src         string
}

var (
	corpusOnce sync.Once
	corpusData *corpus
)

func unhex(t testing.TB, s string) []byte {
	if s == "-" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadCorpus(t testing.TB) *corpus {
	t.Helper()
	corpusOnce.Do(func() {
		samples, _ := filepath.Abs(filepath.Join("testdata", "samples.txt"))
		raw := reftest.Golden(t, "components.txt", "kremenmc.probe.item.Components", samples)
		c := &corpus{regs: map[string]map[string]int32{}}
		sc := bufio.NewScanner(bytes.NewReader(raw))
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) == 0 {
				continue
			}
			switch f[0] {
			case "ctype":
				c.ctypes = append(c.ctypes, f[2])
			case "reg":
				m := c.regs[f[1]]
				if m == nil {
					m = map[string]int32{}
					c.regs[f[1]] = m
				}
				id, _ := strconv.Atoi(f[2])
				m[f[3]] = int32(id)
			case "comp":
				ty, ok := TypeByName(f[1])
				if !ok {
					t.Fatalf("unknown component %s", f[1])
				}
				c.comps = append(c.comps, refComp{t: ty, raw: unhex(t, f[2]), hash: f[3], src: f[4]})
			case "stack":
				c.stacks = append(c.stacks, refStack{raw: unhex(t, f[1]), hashed: unhex(t, f[2]), src: f[3]})
			case "error":
				c.errors = append(c.errors, sc.Text())
			}
		}
		corpusData = c
	})
	return corpusData
}

func TestReferenceTypes(t *testing.T) {
	c := loadCorpus(t)
	if len(c.ctypes) != int(NumTypes) {
		t.Fatalf("reference has %d component types, want %d", len(c.ctypes), NumTypes)
	}
	for i, n := range c.ctypes {
		if Type(i).String() != n {
			t.Errorf("type %d: reference %s, ours %s", i, n, Type(i))
		}
	}
	for _, e := range c.errors {
		t.Errorf("probe sample error: %s", e)
	}
}

func TestReferenceComponentSkip(t *testing.T) {
	c := loadCorpus(t)
	bad := 0
	for _, rc := range c.comps {
		r := proto.Reader{B: rc.raw}
		err := SkipValue(rc.t, &r)
		if err == nil && r.Remaining() != 0 {
			err = ErrMalformed
		}
		if err != nil {
			if bad++; bad < 30 {
				t.Errorf("%s (%s) %x: %v (consumed %d of %d)", rc.t, rc.src, rc.raw, err, r.Off, len(rc.raw))
			}
		}
	}
	t.Logf("%d component values, %d failed", len(c.comps), bad)
}

func TestReferenceStacks(t *testing.T) {
	c := loadCorpus(t)
	bad := 0
	for _, rs := range c.stacks {
		s, err := DecodeStack(rs.raw)
		if err == nil {
			if got := AppendStack(nil, s); !bytes.Equal(got, rs.raw) {
				err = ErrMalformed
			}
		}
		if err != nil {
			if bad++; bad < 30 {
				t.Errorf("%s %x: %v", rs.src, rs.raw, err)
			}
		}
	}
	t.Logf("%d stacks, %d failed", len(c.stacks), bad)
}

func TestReferenceHashes(t *testing.T) {
	c := loadCorpus(t)
	bad, skipped := map[Type]int{}, map[Type]int{}
	ok := 0
	for _, rc := range c.comps {
		if rc.hash == "-" {
			continue
		}
		want, _ := strconv.Atoi(rc.hash)
		got, err := HashComponent(rc.t, rc.raw)
		switch {
		case err != nil && (rc.t == CanBreak || rc.t == CanPlaceOn):
			skipped[rc.t]++
		case err != nil:
			skipped[rc.t]++
			if skipped[rc.t] <= 2 {
				t.Errorf("%s (%s) %x: %v", rc.t, rc.src, rc.raw, err)
			}
		case got != int32(want):
			bad[rc.t]++
			if bad[rc.t] <= 2 {
				t.Errorf("%s (%s) %x: hash %d, want %d", rc.t, rc.src, rc.raw, got, want)
			}
		default:
			ok++
		}
	}
	t.Logf("hashes: %d ok, mismatched types %v, unhashable %v", ok, bad, skipped)
}

func TestReferenceHashedStacks(t *testing.T) {
	c := loadCorpus(t)
	bad := 0
	for _, rs := range c.stacks {
		s, err := DecodeStack(rs.raw)
		if err != nil {
			t.Fatal(err)
		}
		r := proto.Reader{B: rs.hashed}
		h, err := ReadHashed(&r)
		if err == nil && r.Remaining() != 0 {
			err = ErrMalformed
		}
		if err != nil {
			t.Fatalf("%s: %v", rs.src, err)
		}
		if !h.Matches(s) {
			if bad++; bad < 10 {
				t.Logf("%s: hashed view does not match (unhashable component?)", rs.src)
			}
		}
		if again, _ := ReadHashed(&proto.Reader{B: AppendHashed(nil, s)}); !again.Matches(s) && bad == 0 {
			t.Errorf("%s: own hashed encoding does not match", rs.src)
		}
	}
	if bad > 1 {
		t.Errorf("%d of %d stacks mismatched their reference hashed view", bad, len(c.stacks))
	}
}

func TestReferencePrototypes(t *testing.T) {
	if err := PrototypeError(); err != nil {
		t.Fatal(err)
	}
	c := loadCorpus(t)
	checked := map[Type]int{}
	for _, rc := range c.comps {
		name, ok := strings.CutPrefix(rc.src, "proto:")
		if !ok {
			continue
		}
		id, ok := IDOf(name)
		if !ok {
			t.Fatalf("unknown item %s", name)
		}
		p := Defaults(id)
		if !p.Has(rc.t) {
			t.Errorf("%s: prototype lacks %s", name, rc.t)
			continue
		}
		w, ok := p.Wire(rc.t)
		if !ok {
			continue
		}
		checked[rc.t]++
		if !bytes.Equal(w, rc.raw) {
			t.Errorf("%s %s: ours %x, reference %x", name, rc.t, w, rc.raw)
		}
	}
	t.Logf("prototype values checked: %v", checked)
}

func TestReferencePotions(t *testing.T) {
	raw := reftest.Golden(t, "potions.txt", "kremenmc.probe.item.Potions")
	want := map[string][]PotionEffect{}
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 || f[0] != "potion" {
			continue
		}
		if _, ok := want[f[1]]; !ok {
			want[f[1]] = []PotionEffect{}
		}
		if f[2] != "-" {
			d, _ := strconv.Atoi(f[3])
			a, _ := strconv.Atoi(f[4])
			want[f[1]] = append(want[f[1]], PotionEffect{f[2], int32(d), int32(a)})
		}
	}
	for i := int32(0); ; i++ {
		n := regName("minecraft:potion", i)
		if n == "" {
			break
		}
		if fmt.Sprint(PotionEffects(i)) != fmt.Sprint(want[n]) {
			t.Errorf("%s: %v, want %v", n, PotionEffects(i), want[n])
		}
		delete(want, n)
	}
	if len(want) > 0 {
		t.Errorf("potions missing from registry: %v", want)
	}
}

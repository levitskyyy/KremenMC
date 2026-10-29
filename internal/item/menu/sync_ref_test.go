package menu

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/reftest"
)

type syncCase struct {
	id                 int
	creative           bool
	slots, carried     string
	slot, button, kind int
	delta              int
	pred, predCarried  string
}

type recSender struct{ out []string }

func (r *recSender) SetSlot(c, st int32, slot int, s item.Stack) {
	r.out = append(r.out, fmt.Sprintf("slot:%d:%d:%d:%s", c, st, slot, hexOf(s)))
}
func (r *recSender) SetCursor(s item.Stack) { r.out = append(r.out, "cursor:"+hexOf(s)) }
func (r *recSender) SetContent(c, st int32, items []item.Stack, carried item.Stack) {
	hs := make([]string, len(items))
	for i, s := range items {
		hs[i] = hexOf(s)
	}
	r.out = append(r.out, fmt.Sprintf("content:%d:%d:%s:%s", c, st, strings.Join(hs, "."), hexOf(carried)))
}

func specOf(pal map[int]item.Stack, s item.Stack) string {
	if s.IsEmpty() {
		return "e"
	}
	for i := range len(pal) {
		if item.SameItemSameComponents(pal[i], s) {
			return fmt.Sprintf("%d:%d", i, s.Count)
		}
	}
	panic("stack not in palette")
}

func loadPalette(t *testing.T) map[int]item.Stack {
	b, err := os.ReadFile(filepath.Join("testdata", "clicks_out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	pal := map[int]item.Stack{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 3 && f[0] == "palhex" {
			i, _ := strconv.Atoi(f[1])
			raw, _ := hex.DecodeString(f[2])
			pal[i], _ = item.DecodeStack(raw)
		}
	}
	return pal
}

func applySpec(pal map[int]item.Stack, spec string) item.Stack {
	if spec == "e" {
		return item.Stack{}
	}
	pc := strings.Split(spec, ":")
	i, _ := strconv.Atoi(pc[0])
	n, _ := strconv.Atoi(pc[1])
	return pal[i].WithCount(int32(n))
}

func setup(pal map[int]item.Stack, c syncCase) (*Menu, *fakePlayer) {
	m := NewPlayer()
	if c.slots != "-" {
		for _, e := range strings.Split(c.slots, ",") {
			kv := strings.SplitN(e, "=", 2)
			i, _ := strconv.Atoi(kv[0])
			m.Items[i] = applySpec(pal, kv[1])
		}
	}
	if c.carried != "-" {
		m.Carried = applySpec(pal, c.carried)
	}
	return m, &fakePlayer{creative: c.creative}
}

func hashedOf(s item.Stack) item.Hashed {
	h, err := item.ReadHashed(&proto.Reader{B: item.AppendHashed(nil, s)})
	if err != nil {
		panic(err)
	}
	return h
}

func TestReferenceSync(t *testing.T) {
	pal := loadPalette(t)
	r := rand.New(rand.NewPCG(777, 1))
	base := genCases(1500)
	cases := make([]syncCase, 0, len(base))
	for _, b := range base {
		cl := strings.Split(b.clicks, ",")[0]
		x := strings.Split(cl, "/")
		s, _ := strconv.Atoi(x[0])
		bt, _ := strconv.Atoi(x[1])
		k, _ := strconv.Atoi(x[2])
		if k == int(QuickCraft) {
			continue
		}
		c := syncCase{id: b.id, creative: b.creative, slots: b.slots, carried: b.carried, slot: s, button: bt, kind: k}
		m, p := setup(pal, c)
		before := slices.Clone(m.Items)
		m.Click(s, int32(bt), int32(k), p)
		var pred []string
		for i := range m.Items {
			if !item.Matches(before[i], m.Items[i]) {
				pred = append(pred, fmt.Sprintf("%d=%s", i, specOf(pal, m.Items[i])))
			}
		}
		pc := specOf(pal, m.Carried)
		switch r.IntN(8) {
		case 0:
			c.delta = -1
		case 1:
			if len(pred) > 0 {
				pred = pred[1:]
			}
		case 2:
			i := 9 + r.IntN(36)
			pred = append(pred, fmt.Sprintf("%d=%s", i, specOf(pal, pal[0].WithCount(int32(1+r.IntN(5))))))
		case 3:
			pc = "e"
		}
		c.pred, c.predCarried = "-", pc
		if len(pred) > 0 {
			c.pred = strings.Join(pred, ",")
		}
		cases = append(cases, c)
	}
	in, _ := filepath.Abs(filepath.Join("testdata", "sync_in.txt"))
	if os.Getenv("KREMENMC_REF") == "1" {
		var b bytes.Buffer
		for i, p := range palette {
			fmt.Fprintf(&b, "palette %d %s\n", i, p)
		}
		for _, c := range cases {
			gm := "s"
			if c.creative {
				gm = "c"
			}
			fmt.Fprintf(&b, "case %d %s %s %s %d/%d/%d %d %s %s\n", c.id, gm, c.slots, c.carried, c.slot, c.button, c.kind, c.delta, c.pred, c.predCarried)
		}
		if err := os.WriteFile(in, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden := reftest.Golden(t, "sync_out.txt", "kremenmc.probe.item.Sync", in)
	want := map[int]string{}
	sc := bufio.NewScanner(bytes.NewReader(golden))
	sc.Buffer(nil, 1<<22)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) > 0 && f[0] == "error" {
			t.Errorf("probe: %s", sc.Text())
		}
		if len(f) < 3 || f[0] != "sync" {
			continue
		}
		id, _ := strconv.Atoi(f[1])
		rest := []string{f[2]}
		for _, x := range f[4:] {
			// Recipe unlocks on inventory change are out of scope here.
			if x != "ClientboundRecipeBookAddPacket" {
				rest = append(rest, x)
			}
		}
		want[id] = strings.Join(rest, " ")
	}
	bad := 0
	for _, c := range cases {
		m, p := setup(pal, c)
		y := &Sync{}
		var rec recSender
		y.SendAll(m, &rec)
		y.SendAll(m, &rec)
		rec.out = nil
		start := y.StateID()
		changed := map[int]item.Hashed{}
		if c.pred != "-" {
			for _, e := range strings.Split(c.pred, ",") {
				kv := strings.SplitN(e, "=", 2)
				i, _ := strconv.Atoi(kv[0])
				changed[i] = hashedOf(applySpec(pal, kv[1]))
			}
		}
		y.Click(m, start+int32(c.delta), c.slot, int32(c.button), int32(c.kind), changed, hashedOf(applySpec(pal, c.predCarried)), p, false, &rec)
		got := strings.Join(append(append([]string{fmt.Sprintf("state=%d", start)}, rec.out...), fmt.Sprintf("end=%d", y.StateID())), " ")
		if got != want[c.id] {
			if bad++; bad <= 5 {
				t.Errorf("case %d %+v\n got  %s\n want %s", c.id, c, got, want[c.id])
			}
		}
	}
	t.Logf("%d sync cases, %d mismatched", len(cases), bad)
}

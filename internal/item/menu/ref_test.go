package menu

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/reftest"
)

var palette = []string{
	"minecraft:stone",
	`minecraft:stone[custom_name="A"]`,
	"minecraft:ender_pearl",
	"minecraft:diamond_sword",
	"minecraft:diamond_sword[damage=10]",
	"minecraft:diamond_helmet",
	"minecraft:diamond_chestplate",
	"minecraft:diamond_leggings",
	"minecraft:diamond_boots",
	"minecraft:elytra",
	"minecraft:carved_pumpkin",
	"minecraft:shield",
	"minecraft:totem_of_undying",
	`minecraft:diamond_helmet[enchantments={"minecraft:binding_curse":1}]`,
	"minecraft:stone[max_stack_size=99]",
	"minecraft:apple",
	"minecraft:turtle_helmet",
	"minecraft:white_carpet",
	"minecraft:stone[max_stack_size=1]",
	"minecraft:snowball",
	"minecraft:player_head",
	"minecraft:diamond_sword[damage=10,custom_name=\"B\"]",
}

// counts per palette entry kind: a stack size drawn up to this bound.
func maxCount(i int) int {
	switch i {
	case 2, 19:
		return 16
	case 3, 4, 5, 6, 7, 8, 9, 11, 12, 13, 16, 18, 20, 21:
		return 1
	case 14:
		return 99
	}
	return 64
}

type clickCase struct {
	id       int
	creative bool
	selected int
	carried  string
	slots    string
	clicks   string
}

func genCases(n int) []clickCase {
	r := rand.New(rand.NewPCG(26, 3))
	slot := func() int { return 5 + r.IntN(41) }
	stack := func() string {
		p := r.IntN(len(palette))
		return fmt.Sprintf("%d:%d", p, 1+r.IntN(maxCount(p)))
	}
	out := make([]clickCase, n)
	for k := range out {
		c := clickCase{id: k, creative: r.IntN(5) == 0, selected: r.IntN(9), carried: "-", slots: "-"}
		if r.IntN(3) == 0 {
			c.carried = stack()
		}
		var ss []string
		used := map[int]bool{}
		for range r.IntN(14) {
			s := slot()
			if !used[s] {
				used[s] = true
				ss = append(ss, fmt.Sprintf("%d=%s", s, stack()))
			}
		}
		if len(ss) > 0 {
			c.slots = strings.Join(ss, ",")
		}
		var cl []string
		add := func(s, b, t int) { cl = append(cl, fmt.Sprintf("%d/%d/%d", s, b, t)) }
		for range 1 + r.IntN(6) {
			switch r.IntN(12) {
			case 0, 1, 2:
				s := slot()
				if r.IntN(8) == 0 {
					s = Outside
				}
				add(s, r.IntN(2), int(Pickup))
			case 3, 4:
				s := slot()
				if r.IntN(10) == 0 {
					s = Outside
				}
				add(s, r.IntN(2), int(QuickMove))
			case 5:
				b := r.IntN(10)
				if b == 9 {
					b = 40
				}
				add(slot(), b, int(Swap))
			case 6:
				add(slot(), 2, int(Clone))
			case 7:
				add(slot(), r.IntN(2), int(Throw))
			case 8, 9:
				t := r.IntN(3)
				if r.IntN(10) == 0 {
					t = 3
				}
				add(Outside, t*4, int(QuickCraft))
				for range r.IntN(5) {
					add(slot(), t*4+1, int(QuickCraft))
				}
				if r.IntN(8) == 0 {
					add(slot(), 0, int(Pickup))
				}
				add(Outside, t*4+2, int(QuickCraft))
			case 10:
				add(slot(), r.IntN(2), int(PickupAll))
			case 11:
				add(-1, 0, int(Pickup))
			}
		}
		c.clicks = strings.Join(cl, ",")
		out[k] = c
	}
	return out
}

type fakePlayer struct {
	creative bool
	selected int
	drops    []string
}

func (p *fakePlayer) InfiniteMaterials() bool { return p.creative }
func (p *fakePlayer) Creative() bool          { return p.creative }
func (p *fakePlayer) EntityType() int32 {
	e, _ := data.Canonical().EntityByName("player")
	return int32(e.ID)
}
func (p *fakePlayer) SelectedSlot() int { return p.selected }
func (p *fakePlayer) Drop(s item.Stack, throw bool) {
	if s.IsEmpty() {
		return
	}
	f := ":f"
	if throw {
		f = ":t"
	}
	p.drops = append(p.drops, hexOf(s)+f)
}

func hexOf(s item.Stack) string { return hex.EncodeToString(item.AppendStack(nil, s)) }

func TestReferenceClicks(t *testing.T) {
	cases := genCases(3000)
	in, err := filepath.Abs(filepath.Join("testdata", "clicks_in.txt"))
	if err != nil {
		t.Fatal(err)
	}
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
			fmt.Fprintf(&b, "case %d %s %d %s %s %s\n", c.id, gm, c.selected, c.carried, c.slots, c.clicks)
		}
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(in, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden := reftest.Golden(t, "clicks_out.txt", "kremenmc.probe.item.Clicks", in)
	pal := map[int]item.Stack{}
	results := map[int][]string{}
	sc := bufio.NewScanner(bytes.NewReader(golden))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) >= 3 && f[0] == "palhex":
			i, _ := strconv.Atoi(f[1])
			b, _ := hex.DecodeString(f[2])
			s, err := item.DecodeStack(b)
			if err != nil {
				t.Fatal(err)
			}
			pal[i] = s
		case len(f) == 5 && f[0] == "result":
			id, _ := strconv.Atoi(f[1])
			results[id] = f[2:]
		case len(f) > 0 && f[0] == "error":
			t.Errorf("probe: %s", sc.Text())
		}
	}
	stack := func(spec string) item.Stack {
		pc := strings.Split(spec, ":")
		i, _ := strconv.Atoi(pc[0])
		n, _ := strconv.Atoi(pc[1])
		return pal[i].WithCount(int32(n))
	}
	bad := 0
	for _, c := range cases {
		want, ok := results[c.id]
		if !ok {
			t.Fatalf("case %d missing from golden", c.id)
		}
		m := NewPlayer()
		p := &fakePlayer{creative: c.creative, selected: c.selected}
		if c.slots != "-" {
			for _, e := range strings.Split(c.slots, ",") {
				kv := strings.SplitN(e, "=", 2)
				i, _ := strconv.Atoi(kv[0])
				m.Items[i] = stack(kv[1])
			}
		}
		if c.carried != "-" {
			m.Carried = stack(c.carried)
		}
		for _, cl := range strings.Split(c.clicks, ",") {
			x := strings.Split(cl, "/")
			s, _ := strconv.Atoi(x[0])
			b, _ := strconv.Atoi(x[1])
			k, _ := strconv.Atoi(x[2])
			m.Click(s, int32(b), int32(k), p)
		}
		hs := make([]string, len(m.Items))
		for i, s := range m.Items {
			hs[i] = hexOf(s)
		}
		drops := "-"
		if len(p.drops) > 0 {
			drops = strings.Join(p.drops, ",")
		}
		got := []string{strings.Join(hs, ","), hexOf(m.Carried), drops}
		if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			if bad++; bad <= 10 {
				t.Errorf("case %d (gm creative=%v sel=%d carried=%s slots=%s clicks=%s):\n slots  %s\n want   %s\n carried %s want %s\n drops %s want %s",
					c.id, c.creative, c.selected, c.carried, c.slots, c.clicks, diffSlots(got[0], want[0]), "", got[1], want[1], got[2], want[2])
			}
		}
	}
	t.Logf("%d click cases, %d mismatched", len(cases), bad)
}

func diffSlots(a, b string) string {
	x, y := strings.Split(a, ","), strings.Split(b, ",")
	var out []string
	for i := range x {
		if i < len(y) && x[i] != y[i] {
			out = append(out, fmt.Sprintf("[%d] ours=%s ref=%s", i, x[i], y[i]))
		}
	}
	return strings.Join(out, " ")
}

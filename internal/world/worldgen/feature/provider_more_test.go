package feature

import (
	"bufio"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

// TestStatesGolden replays testdata/feat_states.txt (tools/refprobe
// kremenmc.probe.feat_ore.StateProbe): registered block state providers with
// the full resulting state and the random's next long.
func TestStatesGolden(t *testing.T) {
	rg, err := theRegistry()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/feat_states.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		in, want, _ := strings.Cut(sc.Text(), " => ")
		fs := strings.SplitN(in, " ", 6)
		seed, _ := strconv.ParseInt(fs[1], 10, 64)
		p, err := rg.parseStateProvider(json.RawMessage(strconv.Quote(fs[5])))
		if err != nil {
			t.Fatal(err)
		}
		r := rng.NewWorldgenRandom(rng.NewXoroshiro(seed))
		st := p.State(nil, r, Pos{})
		reg := data.Canonical()
		name := strings.TrimPrefix(reg.BlockOf(st).Name, "minecraft:")
		var props []string
		for _, pr := range reg.BlockOf(st).Props {
			v, _ := reg.Value(st, pr.Name)
			props = append(props, pr.Name+"="+v)
		}
		got := "Block{minecraft:" + name + "}"
		if len(props) > 0 {
			got += "[" + strings.Join(props, ",") + "]"
		}
		got += " | " + strconv.FormatInt(r.NextLong(), 10)
		if got != want {
			t.Errorf("%s\n got %s\nwant %s", in, got, want)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no cases")
	}
}

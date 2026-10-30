package rptest

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/structure"
)

// TestRPSynth (KREMENMC_RPSYNTH="base.txt>out.txt>key=value,...") writes an
// RPScene input from a scene with the piece's fields replaced (Template,
// Rotation, Mirror, VerticalPlacement, TPY, Properties.<name>) and the box
// recomputed.
func TestRPSynth(t *testing.T) {
	spec := os.Getenv("KREMENMC_RPSYNTH")
	if spec == "" {
		t.Skip("KREMENMC_RPSYNTH not set")
	}
	for _, one := range strings.Split(spec, ";") {
		a := strings.Split(one, ">")
		b, err := os.ReadFile(a[0])
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		for i, l := range lines {
			if !strings.HasPrefix(l, "START ") {
				continue
			}
			st := parseSNBT(t, l[6:]).(nbt.Compound)
			pc := st["Children"].(nbt.List).Items[0].(nbt.Compound)
			props := pc["Properties"].(nbt.Compound)
			for _, kv := range strings.Split(a[2], ",") {
				k, v, _ := strings.Cut(kv, "=")
				switch {
				case strings.HasPrefix(k, "p."):
					if k == "p.mossiness" {
						var f float32
						fmt.Sscan(v, &f)
						props[k[2:]] = f
					} else {
						props[k[2:]] = int8(map[string]int{"0": 0, "1": 1}[v])
					}
				case k == "TPY":
					var y int32
					fmt.Sscan(v, &y)
					pc[k] = y
				default:
					pc[k] = v
				}
			}
			s, err := structure.DecodeStart(st)
			if err != nil {
				t.Fatal(err)
			}
			pc["BB"] = s.Pieces[0].Box().IntArray()
			lines[i] = "START " + quoteSNBT(st)
			bx := s.Pieces[0].Box()
			for j, l2 := range lines {
				if strings.HasPrefix(l2, "CHUNKS") {
					var cs []string
					for cz := bx.MinZ >> 4; cz <= bx.MaxZ>>4; cz++ {
						for cx := bx.MinX >> 4; cx <= bx.MaxX>>4; cx++ {
							cs = append(cs, fmt.Sprintf("%d,%d", cx, cz))
						}
					}
					lines[j] = "CHUNKS " + strings.Join(cs, " ")
				}
			}
			t.Logf("%s: box %v", a[1], s.Pieces[0].Box())
		}
		if err := os.WriteFile(a[1], []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func quoteSNBT(v any) string {
	var sb strings.Builder
	var w func(any)
	w = func(v any) {
		switch x := v.(type) {
		case nbt.Compound:
			sb.WriteByte('{')
			first := true
			for _, k := range sortedKeys(x) {
				if !first {
					sb.WriteByte(',')
				}
				first = false
				fmt.Fprintf(&sb, "%q:", k)
				w(x[k])
			}
			sb.WriteByte('}')
		case nbt.List:
			sb.WriteByte('[')
			for i, it := range x.Items {
				if i > 0 {
					sb.WriteByte(',')
				}
				w(it)
			}
			sb.WriteByte(']')
		case []int32:
			sb.WriteString("[I;")
			for i, n := range x {
				if i > 0 {
					sb.WriteByte(',')
				}
				fmt.Fprint(&sb, n)
			}
			sb.WriteByte(']')
		case string:
			fmt.Fprintf(&sb, "%q", x)
		case int8:
			fmt.Fprintf(&sb, "%db", x)
		case int16:
			fmt.Fprintf(&sb, "%ds", x)
		case int32:
			fmt.Fprint(&sb, x)
		case int64:
			fmt.Fprintf(&sb, "%dL", x)
		case float32:
			fmt.Fprintf(&sb, "%gf", x)
		case float64:
			fmt.Fprintf(&sb, "%gd", x)
		}
	}
	w(v)
	return sb.String()
}

func sortedKeys(c nbt.Compound) []string {
	ks := make([]string, 0, len(c))
	for k := range c {
		ks = append(ks, k)
	}
	for i := range ks {
		for j := i + 1; j < len(ks); j++ {
			if ks[j] < ks[i] {
				ks[i], ks[j] = ks[j], ks[i]
			}
		}
	}
	return ks
}

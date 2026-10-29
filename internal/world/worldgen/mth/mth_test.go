package mth

import (
	"bufio"
	"bytes"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

func TestReference(t *testing.T) {
	data := reftest.Golden(t, "mth.txt", "kremenmc.probe.noise.MthProbe", "7", "16000")
	sc := bufio.NewScanner(bytes.NewReader(data))
	n, fails := 0, 0
	for sc.Scan() {
		line := sc.Text()
		lhs, want, _ := strings.Cut(line, " = ")
		f := strings.Fields(lhs)
		d := make([]float64, len(f)-1)
		fl := make([]float32, len(f)-1)
		for i, s := range f[1:] {
			b, err := strconv.ParseUint(s, 16, 64)
			if err != nil {
				t.Fatal(err)
			}
			d[i] = math.Float64frombits(b)
			fl[i] = math.Float32frombits(uint32(b))
		}
		hd := func(v float64) string {
			if v != v {
				return "nan"
			}
			return strconv.FormatUint(math.Float64bits(v), 16)
		}
		hf := func(v float32) string {
			if v != v {
				return "nan"
			}
			return strconv.FormatUint(uint64(math.Float32bits(v)), 16)
		}
		var got string
		switch f[0] {
		case "floor":
			got = strconv.Itoa(int(Floor(d[0])))
		case "lfloor":
			got = strconv.FormatInt(LFloor(d[0]), 10)
		case "ceil":
			got = strconv.Itoa(int(Ceil(d[0])))
		case "floorf":
			got = strconv.Itoa(int(FloorF(fl[0])))
		case "log":
			got = hd(StrictLog(d[0]))
		case "lerp":
			got = hd(Lerp(d[0], d[1], d[2]))
		case "lerpf":
			got = hf(LerpF(fl[0], fl[1], fl[2]))
		case "lerp3":
			got = hd(Lerp3(d[0], d[1], d[2], d[3], d[4], d[5], d[6], d[7], d[8], d[9], d[10]))
		case "lerp3f":
			got = hf(Lerp3F(fl[0], fl[1], fl[2], fl[3], fl[4], fl[5], fl[6], fl[7], fl[8], fl[9], fl[10]))
		case "smooth":
			got = hf(SmoothStep(fl[0])) + " " + hf(SmoothStepDerivative(fl[0]))
		case "clampedlerp":
			got = hd(ClampedLerp(d[0], d[1], d[2]))
		case "clampedlerpf":
			got = hf(ClampedLerpF(fl[0], fl[1], fl[2]))
		case "invlerp":
			got = hd(InverseLerp(d[0], d[1], d[2]))
		case "map":
			got = hd(Map(d[0], d[1], d[2], d[3], d[4])) + " " + hd(ClampedMap(d[0], d[1], d[2], d[3], d[4]))
		case "mapf":
			got = hf(MapF(fl[0], fl[1], fl[2], fl[3], fl[4])) + " " + hf(ClampedMapF(fl[0], fl[1], fl[2], fl[3], fl[4]))
		case "clamp":
			got = hd(Clamp(d[0], d[1], d[2]))
		case "clampf":
			got = hf(ClampF(fl[0], fl[1], fl[2]))
		case "frac":
			got = hd(Frac(d[0]))
		default:
			t.Fatalf("unknown %q", line)
		}
		n++
		want = canonNaN(want, f[0])
		if got != want {
			fails++
			if fails < 30 {
				t.Errorf("%s: got %s", line, got)
			}
		}
	}
	if fails > 0 {
		t.Errorf("%d/%d mismatches", fails, n)
	}
}

// canonNaN replaces NaN bit patterns in want by "nan"; NaN sign and payload
// are not specified by Java or Go arithmetic.
func canonNaN(want, fn string) string {
	if _, err := strconv.Atoi(strings.Fields(want)[0]); err == nil && (strings.Contains(fn, "floor") || fn == "ceil") {
		return want
	}
	ws := strings.Fields(want)
	for i, w := range ws {
		b, _ := strconv.ParseUint(w, 16, 64)
		if (floatFns[fn] && math.IsNaN(float64(math.Float32frombits(uint32(b))))) ||
			(!floatFns[fn] && math.IsNaN(math.Float64frombits(b))) {
			ws[i] = "nan"
		}
	}
	return strings.Join(ws, " ")
}

var floatFns = map[string]bool{"lerpf": true, "lerp3f": true, "smooth": true, "clampedlerpf": true, "mapf": true, "clampf": true}

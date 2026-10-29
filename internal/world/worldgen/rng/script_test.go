package rng

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/reftest"
)

func pi64(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func pi32(t *testing.T, s string) int32 { return int32(pi64(t, s)) }

func pbits(t *testing.T, s string) uint64 {
	t.Helper()
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func pname(t *testing.T, s string) string {
	t.Helper()
	b, err := hex.DecodeString(strings.TrimPrefix(s, "h"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hexd(v float64) string { return strconv.FormatUint(math.Float64bits(v), 16) }
func hexf(v float32) string { return strconv.FormatUint(uint64(math.Float32bits(v)), 16) }

// runScript replays a reference RngScript transcript and checks every result.
func runScript(t *testing.T, data []byte) int {
	var srcs []Source
	var pos []Positional
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n, fails := 0, 0
	check := func(line, got, want string) {
		n++
		if got != want {
			fails++
			if fails <= 20 {
				t.Errorf("%s: got %s", line, got)
			}
		}
	}
	for sc.Scan() {
		line := sc.Text()
		lhs, want, _ := strings.Cut(line, " = ")
		f := strings.Fields(lhs)
		switch f[0] {
		case "mix":
			check(line, strconv.FormatInt(MixStafford13(pi64(t, f[1])), 10), want)
		case "up", "upu", "md5":
			var s Seed128
			switch f[0] {
			case "up":
				s = UpgradeSeedTo128bit(pi64(t, f[1]))
			case "upu":
				s = UpgradeSeedTo128bitUnmixed(pi64(t, f[1]))
			default:
				s = SeedFromHashOf(pname(t, f[1]))
			}
			check(line, strconv.FormatInt(s.Lo, 10)+" "+strconv.FormatInt(s.Hi, 10), want)
		case "getseed":
			check(line, strconv.FormatInt(GetSeed(pi32(t, f[1]), pi32(t, f[2]), pi32(t, f[3])), 10), want)
		case "pos":
			pos = append(pos, srcs[pi64(t, f[2])].ForkPositional())
		case "new":
			var s Source
			switch f[2] {
			case "legacy", "single":
				s = NewLegacy(pi64(t, f[3]))
			case "xoro":
				s = NewXoroshiro(pi64(t, f[3]))
			case "xoro128":
				s = NewXoroshiro128(pi64(t, f[3]), pi64(t, f[4]))
			case "wg":
				a := AlgorithmLegacy
				if f[3] == "xoro" {
					a = AlgorithmXoroshiro
				}
				s = NewWorldgenRandom(a.NewInstance(pi64(t, f[4])))
			case "fork":
				s = srcs[pi64(t, f[3])].Fork()
			case "at":
				s = pos[pi64(t, f[3])].At(pi32(t, f[4]), pi32(t, f[5]), pi32(t, f[6]))
			case "hash":
				s = pos[pi64(t, f[3])].FromHashOf(pname(t, f[4]))
			case "fseed":
				s = pos[pi64(t, f[3])].FromSeed(pi64(t, f[4]))
			case "slime":
				s = SeedSlimeChunk(pi32(t, f[3]), pi32(t, f[4]), pi64(t, f[5]), pi64(t, f[6]))
			default:
				t.Fatalf("unknown source %q", line)
			}
			if int(pi64(t, f[1])) != len(srcs) {
				t.Fatalf("bad id in %q", line)
			}
			srcs = append(srcs, s)
		case "op":
			s := srcs[pi64(t, f[1])]
			a := f[3:]
			var got string
			switch f[2] {
			case "int":
				got = strconv.Itoa(int(s.NextInt()))
			case "intn":
				got = strconv.Itoa(int(s.NextIntn(pi32(t, a[0]))))
			case "intr":
				got = strconv.Itoa(int(s.NextIntRange(pi32(t, a[0]), pi32(t, a[1]))))
			case "intbi":
				got = strconv.Itoa(int(s.NextIntBetweenInclusive(pi32(t, a[0]), pi32(t, a[1]))))
			case "long":
				got = strconv.FormatInt(s.NextLong(), 10)
			case "bool":
				got = "0"
				if s.NextBool() {
					got = "1"
				}
			case "float":
				got = hexf(s.NextFloat())
			case "double":
				got = hexd(s.NextDouble())
			case "gauss":
				got = hexd(s.NextGaussian())
			case "tri":
				got = hexd(s.Triangle(math.Float64frombits(pbits(t, a[0])), math.Float64frombits(pbits(t, a[1]))))
			case "trif":
				got = hexf(s.TriangleF(math.Float32frombits(uint32(pbits(t, a[0]))), math.Float32frombits(uint32(pbits(t, a[1])))))
			case "consume":
				s.Consume(int(pi64(t, a[0])))
				got = "-"
			case "setseed":
				s.SetSeed(pi64(t, a[0]))
				got = "-"
			case "intn256x":
				var sb strings.Builder
				for i := pi64(t, a[0]); i > 0; i-- {
					if sb.Len() > 0 {
						sb.WriteByte(' ')
					}
					sb.WriteString(strconv.Itoa(int(s.NextIntn(256))))
				}
				got = sb.String()
			default:
				w := s.(*WorldgenRandom)
				got = "-"
				switch f[2] {
				case "wgdeco":
					got = strconv.FormatInt(w.SetDecorationSeed(pi64(t, a[0]), pi32(t, a[1]), pi32(t, a[2])), 10)
				case "wgfeat":
					w.SetFeatureSeed(pi64(t, a[0]), pi32(t, a[1]), pi32(t, a[2]))
				case "wglarge":
					w.SetLargeFeatureSeed(pi64(t, a[0]), pi32(t, a[1]), pi32(t, a[2]))
				case "wgsalt":
					w.SetLargeFeatureWithSalt(pi64(t, a[0]), pi32(t, a[1]), pi32(t, a[2]), pi32(t, a[3]))
				case "wgcount":
					got = strconv.Itoa(w.Count())
				default:
					t.Fatalf("unknown op %q", line)
				}
			}
			check(line, got, want)
		default:
			t.Fatalf("unknown line %q", line)
		}
	}
	if fails > 0 {
		t.Errorf("%d/%d mismatches", fails, n)
	}
	return n
}

func TestReferenceScript(t *testing.T) {
	for _, c := range []struct{ file, seed, steps string }{
		{"rng_script_1.txt", "1", "12000"},
		{"rng_script_2.txt", "-7311", "12000"},
	} {
		t.Run(c.file, func(t *testing.T) {
			n := runScript(t, reftest.Golden(t, c.file, "kremenmc.probe.rng.RngScript", c.seed, c.steps))
			t.Logf("%d checks", n)
		})
	}
}

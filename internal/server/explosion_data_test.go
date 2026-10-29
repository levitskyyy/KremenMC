package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/reftest"
)

// TestExplosionBlockData checks the embedded table covers every state; with
// KREMENMC_REF=1 it is first regenerated from the reference.
func TestExplosionBlockData(t *testing.T) {
	if os.Getenv("KREMENMC_REF") == "1" {
		ref, err := reftest.Open()
		if err != nil {
			t.Fatal(err)
		}
		var raw bytes.Buffer
		if err := ref.Run(&raw, "kremenmc.probe.explosion.BlockData"); err != nil {
			t.Fatal(err)
		}
		gz, err := convertBlockData(raw.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("refdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("refdata", "explosion_blocks.txt.gz"), gz, 0o644); err != nil {
			t.Fatal(err)
		}
		explosionBlocksGz = gz
	}
	tb, err := parseExplosionBlocks(explosionBlocksGz)
	if err != nil {
		t.Fatal(err)
	}
	reg := data.Canonical()
	for _, c := range []struct {
		state string
		flags uint8
	}{
		{"minecraft:stone", exSolidRender | exSturdyUp},
		{"minecraft:air", exAir},
		{"minecraft:water[level=0]", exFluid},
		{"minecraft:oak_planks", exSolidRender | exSturdyUp | exCanBurn},
		{"minecraft:glass", exSturdyUp},
	} {
		s := data.MustState(c.state)
		if tb.flags[s] != c.flags {
			t.Errorf("%s flags %b, want %b", c.state, tb.flags[s], c.flags)
		}
	}
	if r := tb.blocks[reg.BlockID(data.MustState("minecraft:obsidian"))].res; r != 1200 {
		t.Errorf("obsidian resistance %v", r)
	}
	if !tb.blocks[reg.BlockID(data.MustState("minecraft:soul_soil"))].soulBase {
		t.Error("soul soil not a soul fire base")
	}
}

func convertBlockData(raw []byte) ([]byte, error) {
	var out bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&out, gzip.BestCompression)
	sc := bufio.NewScanner(bytes.NewReader(raw))
	var line strings.Builder
	flush := func() {
		if line.Len() > 0 {
			fmt.Fprintln(zw, line.String())
			line.Reset()
		}
	}
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "b":
			flush()
			fmt.Fprintf(&line, "%s %%RES%% %s %s %s ", f[1], f[4], f[5], f[6])
		case "e":
			continue
		case "s":
			bits, err := strconv.ParseUint(f[2], 16, 32)
			if err != nil {
				return nil, err
			}
			s := line.String()
			if i := strings.Index(s, "%RES%"); i >= 0 {
				line.Reset()
				line.WriteString(s[:i] + strconv.FormatFloat(float64(math.Float32frombits(uint32(bits))), 'g', -1, 32) + s[i+5:])
			}
			fl, err := strconv.Atoi(f[4])
			if err != nil {
				return nil, err
			}
			line.WriteString(strconv.FormatInt(int64(fl), 32))
		}
	}
	flush()
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), sc.Err()
}

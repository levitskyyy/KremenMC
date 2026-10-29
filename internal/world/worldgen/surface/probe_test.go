package surface

import (
	"testing"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/world"

	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// probeChunk is one chunk record written by kremenmc.probe.surface.Trace.
type probeChunk struct {
	Tag      string
	X, Z     int32
	Sections int
	Palette  []string
	Blocks   []uint16 // ((section*16+y)*16+z)*16+x
	// WORLD_SURFACE_WG and OCEAN_FLOOR_WG, index z*16+x.
	WorldSurface, OceanFloor [256]int32
}

func (c *probeChunk) At(x, ry, z int) string { return c.Palette[c.Blocks[(ry*16+z)*16+x]] }

func readUTF(r io.Reader) (string, error) {
	var n uint16
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return "", err
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return string(b), err
}

func readProbeChunks(path string) ([]*probeChunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(zr)
	var out []*probeChunk
	for {
		tag, err := readUTF(r)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		c := &probeChunk{Tag: tag}
		var hdr [3]int32
		if err := binary.Read(r, binary.BigEndian, &hdr); err != nil {
			return nil, err
		}
		c.X, c.Z, c.Sections = hdr[0], hdr[1], int(hdr[2])
		var np int32
		if err := binary.Read(r, binary.BigEndian, &np); err != nil {
			return nil, err
		}
		for range np {
			s, err := readUTF(r)
			if err != nil {
				return nil, err
			}
			c.Palette = append(c.Palette, s)
		}
		c.Blocks = make([]uint16, 4096*c.Sections)
		if err := binary.Read(r, binary.BigEndian, c.Blocks); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, c.WorldSurface[:]); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, c.OceanFloor[:]); err != nil {
			return nil, err
		}
		if int(np) == 0 {
			return nil, fmt.Errorf("probe chunk %d,%d: empty palette", c.X, c.Z)
		}
		out = append(out, c)
	}
}

// worldChunk converts a probe record into a world chunk.
func (c *probeChunk) worldChunk(t testing.TB, dim world.Dimension) *world.Chunk {
	t.Helper()
	reg := data.Canonical()
	ids := make([]uint32, len(c.Palette))
	for i, s := range c.Palette {
		id, ok := reg.ParseState(s)
		if !ok {
			t.Fatalf("unknown state %s", s)
		}
		ids[i] = uint32(id)
	}
	plains, _ := reg.BiomeByName("plains")
	w := world.NewChunk(c.X, c.Z, dim, plains)
	vals := make([]uint32, 4096)
	for s := range c.Sections {
		for i := range vals {
			vals[i] = ids[c.Blocks[s*4096+i]]
		}
		w.Sections[s].SetAllBlocks(vals)
	}
	w.RecalcHeightmaps()
	return w
}

func chunkStates(w *world.Chunk) []string {
	reg := data.Canonical()
	out := make([]string, 0, len(w.Sections)*4096)
	for s := range w.Sections {
		for i := range 4096 {
			out = append(out, reg.StateString(data.StateID(w.Sections[s].Blocks.Get(i))))
		}
	}
	return out
}

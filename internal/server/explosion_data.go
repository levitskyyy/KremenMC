package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
)

// Block properties read by explosions and fire placement, dumped from the
// reference by kremenmc.probe.explosion.BlockData (regenerate with
// KREMENMC_REF=1 go test -run TestExplosionBlockData ./internal/server).
// One line per block: name, explosion resistance, ignite odds, burn odds,
// soul fire base (0/1), then one base-32 digit of exFlag* bits per state.
//
//go:embed refdata/explosion_blocks.txt.gz
var explosionBlocksGz []byte

const (
	exSolidRender = 1 << iota
	exSturdyUp
	exAir
	exFluid
	exCanBurn
)

type exBlock struct {
	res          float32
	ignite, burn int32
	soulBase     bool
}

type exTables struct {
	blocks []exBlock // by data.BlockID
	flags  []uint8   // by data.StateID
}

var exData = sync.OnceValue(func() *exTables {
	t, err := parseExplosionBlocks(explosionBlocksGz)
	if err != nil {
		panic(fmt.Errorf("explosion block data: %w", err))
	}
	return t
})

func parseExplosionBlocks(gz []byte) (*exTables, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	reg := data.Canonical()
	t := &exTables{blocks: make([]exBlock, len(reg.Blocks)), flags: make([]uint8, reg.NumStates())}
	sc := bufio.NewScanner(zr)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 6 {
			return nil, fmt.Errorf("bad line %q", sc.Text())
		}
		b, ok := reg.BlockByName(f[0])
		if !ok {
			return nil, fmt.Errorf("unknown block %s", f[0])
		}
		res, err1 := strconv.ParseFloat(f[1], 32)
		ig, err2 := strconv.Atoi(f[2])
		bu, err3 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil || err3 != nil || len(f[5]) != int(b.NumStates) {
			return nil, fmt.Errorf("bad line for %s", f[0])
		}
		t.blocks[b.ID] = exBlock{res: float32(res), ignite: int32(ig), burn: int32(bu), soulBase: f[4] == "1"}
		for i := 0; i < len(f[5]); i++ {
			v, err := strconv.ParseUint(f[5][i:i+1], 32, 8)
			if err != nil {
				return nil, err
			}
			t.flags[int(b.MinState)+i] = uint8(v)
		}
	}
	return t, sc.Err()
}

func exFlags(s data.StateID) uint8 {
	t := exData()
	if int(s) >= len(t.flags) {
		return 0
	}
	return t.flags[s]
}

func exBlockOf(s data.StateID) exBlock {
	t := exData()
	id := data.Canonical().BlockID(s)
	if int(id) >= len(t.blocks) {
		return exBlock{}
	}
	return t.blocks[id]
}

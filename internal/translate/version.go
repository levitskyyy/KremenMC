package translate

import (
	"fmt"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/translate/meta"
)

// chunkFormat describes the chunk section encoding of a protocol.
type chunkFormat uint8

const (
	// fmtFluidCount: section starts with block count and fluid count (26.1+).
	fmtFluidCount chunkFormat = 1 << iota
	// fmtLongArrayLength: paletted containers carry a VarInt long-array length (before 1.21.5).
	fmtLongArrayLength
)

// spec declares one supported protocol. Specs are ordered newest first; the
// step of a spec converts packets between it and the spec before it.
type spec struct {
	protocol int32
	name     string
	data     string
	chunk    chunkFormat
	step     func(*step)
}

var specs = []spec{
	{protocol: packets.Protocol, name: packets.Version, data: "26.3", chunk: fmtFluidCount},
	{protocol: 776, name: "26.2", data: "26.2", chunk: fmtFluidCount, step: step776},
	{protocol: 775, name: "26.1", data: "26.1", chunk: fmtFluidCount, step: step775},
	{protocol: 774, name: "1.21.11", data: "1.21.11", step: step774},
	{protocol: 773, name: "1.21.10", data: "1.21.9", step: step773},
	{protocol: 772, name: "1.21.8", data: "1.21.8", step: step772},
	{protocol: 771, name: "1.21.6", data: "1.21.6", step: step771},
	{protocol: 770, name: "1.21.5", data: "1.21.5", step: step770},
	{protocol: 769, name: "1.21.4", data: "1.21.4", chunk: fmtLongArrayLength, step: step769},
	{protocol: 768, name: "1.21.3", data: "1.21.3", chunk: fmtLongArrayLength, step: step768},
	{protocol: 767, name: "1.21.1", data: "1.21.1", chunk: fmtLongArrayLength, step: step767},
	{protocol: 766, name: "1.20.6", data: "1.20.5", chunk: fmtLongArrayLength, step: step766},
}

// verInfo is a spec with its loaded metadata and packet ID tables.
type verInfo struct {
	*spec
	meta  *meta.Meta
	reg   *data.Registry
	ids   [5][2]map[string]int32
	names [5][2][]string
	step  *step
}

var sdKeys = [5][2]string{
	packets.Handshake:     {"handshaking.toClient", meta.HandshakeServerbound},
	packets.Status:        {meta.StatusClientbound, meta.StatusServerbound},
	packets.Login:         {meta.LoginClientbound, meta.LoginServerbound},
	packets.Configuration: {meta.ConfigClientbound, meta.ConfigServerbound},
	packets.Play:          {meta.PlayClientbound, meta.PlayServerbound},
}

func (v *verInfo) id(s packets.State, d packets.Direction, name string) (int32, bool) {
	id, ok := v.ids[s][d][name]
	return id, ok
}

func (v *verInfo) fingerprint(s packets.State, d packets.Direction, name string) string {
	return v.meta.Packets[sdKeys[s][d]][name]
}

var (
	verMu    sync.Mutex
	verCache = map[int32]*verInfo{}
)

func loadVersion(sp *spec) (*verInfo, error) {
	verMu.Lock()
	defer verMu.Unlock()
	if v, ok := verCache[sp.protocol]; ok {
		return v, nil
	}
	m, err := meta.Load(sp.protocol)
	if err != nil {
		return nil, err
	}
	reg, err := data.Load(sp.data)
	if err != nil {
		return nil, fmt.Errorf("translate %d: %w", sp.protocol, err)
	}
	v := &verInfo{spec: sp, meta: m, reg: reg}
	for s := range sdKeys {
		for d := range 2 {
			order := m.Order[sdKeys[s][d]]
			v.names[s][d] = order
			v.ids[s][d] = make(map[string]int32, len(order))
			for i, n := range order {
				v.ids[s][d][n] = int32(i)
			}
		}
	}
	if sp.step != nil {
		v.step = newStep()
		sp.step(v.step)
	}
	verCache[sp.protocol] = v
	return v, nil
}

func specIndex(protocol int32) int {
	for i := range specs {
		if specs[i].protocol == protocol {
			return i
		}
	}
	return -1
}

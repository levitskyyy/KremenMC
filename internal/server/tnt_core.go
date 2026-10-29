package server

import (
	"math"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/rng"
)

const (
	tntDefaultFuse  = 80
	tntPower        = 4
	metaTNTFuse     = 8 // VarInt; only sent when not the default 80
	metaVarIntSer   = 1
	tntFuseOffsetDY = 0.0625
)

type primedTNT struct {
	fuse   int32
	power  float32
	owner  api.Entity
	portal bool // went through a portal: keeps portal blocks intact
}

// shortFuse is PrimedTnt.getRandomShortFuse.
func shortFuse(fuse int32, r rng.Source) int32 { return r.NextIntn(fuse/4) + fuse/8 }

func tntMeta(fuse int32) []byte {
	if fuse == tntDefaultFuse {
		return entity.EndMeta(nil)
	}
	b := []byte{metaTNTFuse, metaVarIntSer}
	b = proto.AppendVarInt(b, fuse)
	return entity.EndMeta(b)
}

// primedSpawn is the position and initial velocity of TNT primed at p.
func primedSpawn(p api.BlockPos, rnd rng.Source) (pos, vel api.Vec3) {
	a := rnd.NextDouble() * float64(float32(math.Pi)) * 2
	pos = api.Vec3{X: float64(p.X) + 0.5, Y: float64(p.Y), Z: float64(p.Z) + 0.5}
	vel = api.Vec3{X: -fdlibmSin(a) * 0.02, Y: float64(float32(0.2)), Z: -fdlibmCos(a) * 0.02}
	return pos, vel
}

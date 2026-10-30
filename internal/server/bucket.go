package server

import (
	"math"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/entity"
	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/world"
)

// Buckets (minecraft.wiki "Bucket"): an empty bucket picks up the water or
// lava source (or the water of a waterlogged block) or powder snow the
// player looks at; a filled bucket waterlogs the block it hits or places
// its contents on the clicked face. Water placed in an ultrawarm
// dimension evaporates. Fish buckets are not modelled.

// sourceAsSolid makes fluid sources and powder snow hittable for the
// pick-up ray.
type sourceAsSolid struct{ w *world.World }

func (s sourceAsSolid) Block(x, y, z int) data.StateID {
	st := s.w.Block(x, y, z)
	if _, src := world.FluidOf(st); src || isBlock(st, "powder_snow") {
		return bucketStone()
	}
	return st
}

var bucketStone = func() func() data.StateID {
	s := data.MustState("minecraft:stone")
	return func() data.StateID { return s }
}()

func isBlock(s data.StateID, name string) bool {
	b := data.BlockOf(s)
	return b != nil && b.Name == name
}

// bucketRay is the player's view ray within block interaction range.
func (p *Player) bucketRay(src entity.BlockSource) (entity.Hit, bool) {
	loc := p.Location()
	from := loc.Pos
	from.Y += p.eyeY()
	if p.Sneaking() {
		from.Y -= 0.35
	}
	yaw, pitch := float64(loc.Yaw)*math.Pi/180, float64(loc.Pitch)*math.Pi/180
	dir := api.Vec3{X: -math.Sin(yaw) * math.Cos(pitch), Y: -math.Sin(pitch), Z: math.Cos(yaw) * math.Cos(pitch)}
	r := 4.5
	if p.GameMode() == api.Creative {
		r = 5
	}
	to := api.Vec3{X: from.X + dir.X*r, Y: from.Y + dir.Y*r, Z: from.Z + dir.Z*r}
	return entity.Raycast(src, from, to)
}

// useBucket is BucketItem.use / SolidBucketItem for the bucket kinds.
func (p *Player) useBucket(st item.Stack, hand api.Hand) (useResult, item.Stack, bool) {
	w := p.world.Load()
	if w == nil {
		return useFail, item.Stack{}, false
	}
	name := item.NameOf(st.Item)
	creative := p.GameMode() == api.Creative
	if name == "minecraft:bucket" {
		h, ok := p.bucketRay(sourceAsSolid{w.w})
		if !ok {
			return usePass, item.Stack{}, false
		}
		pos := h.Block
		cur := w.w.Block(pos.X, pos.Y, pos.Z)
		k, src := world.FluidOf(cur)
		var result, snd string
		switch {
		case isBlock(cur, "powder_snow"):
			result, snd = "minecraft:powder_snow_bucket", "minecraft:item.bucket.fill_powder_snow"
			w.w.SetBlock(pos.X, pos.Y, pos.Z, data.Air)
		case src && k == world.FluidLava:
			result, snd = "minecraft:lava_bucket", "minecraft:item.bucket.fill_lava"
			w.w.SetBlock(pos.X, pos.Y, pos.Z, data.Air)
		case src && k == world.FluidWater:
			result, snd = "minecraft:water_bucket", "minecraft:item.bucket.fill"
			if dry, ok := data.With(cur, "waterlogged", "false"); ok {
				w.w.SetBlock(pos.X, pos.Y, pos.Z, dry)
			} else if isBlock(cur, "water") {
				w.w.SetBlock(pos.X, pos.Y, pos.Z, data.Air)
			} else {
				// kelp, seagrass, bubble columns hold water that cannot be taken
				p.resync(w, pos)
				return useFail, item.Stack{}, false
			}
		default:
			return usePass, item.Stack{}, false
		}
		w.playSoundAt(pos.Center(), snd, soundPlayers, 1, 1)
		p.exchangeHeld(hand, st, result, creative)
		return useSuccess, item.Stack{}, false
	}

	h, ok := p.bucketRay(w.w)
	if !ok {
		return usePass, item.Stack{}, false
	}
	pos := h.Block
	cur := w.w.Block(pos.X, pos.Y, pos.Z)
	placed := false
	switch name {
	case "minecraft:water_bucket":
		if wet, ok := data.With(cur, "waterlogged", "true"); ok && wet != cur && !w.w.Ultrawarm() {
			w.w.SetBlock(pos.X, pos.Y, pos.Z, wet)
			w.playSoundAt(pos.Center(), "minecraft:item.bucket.empty", soundBlocks, 1, 1)
			placed = true
			break
		}
		fallthrough
	default:
		if !bucketReplaceable(cur) {
			o := faceOffsets[h.Face]
			pos = pos.Add(o[0], o[1], o[2])
			cur = w.w.Block(pos.X, pos.Y, pos.Z)
		}
		if !bucketReplaceable(cur) || pos.Y < w.MinY() || pos.Y >= w.MaxY() {
			p.resync(w, pos)
			return useFail, item.Stack{}, false
		}
		placed = p.emptyBucketAt(w, name, pos, cur)
	}
	if !placed {
		p.resync(w, pos)
		return useFail, item.Stack{}, false
	}
	if creative {
		return useSuccess, item.Stack{}, false
	}
	b, _ := item.IDOf("minecraft:bucket")
	return useSuccess, item.Stack{Item: b, Count: 1}, true
}

// bucketReplaceable: air, fluid, replaceable plants and blocks fluids
// wash away.
func bucketReplaceable(s data.StateID) bool {
	if data.IsAir(s) || data.Flags(s)&(data.FlagReplaceable|data.FlagLiquid) != 0 {
		return true
	}
	return world.FluidBreaks(s)
}

func (p *Player) emptyBucketAt(w *World, name string, pos api.BlockPos, cur data.StateID) bool {
	var st data.StateID
	snd := "minecraft:item.bucket.empty"
	switch name {
	case "minecraft:water_bucket":
		if w.w.Ultrawarm() {
			w.playSoundAt(pos.Center(), "minecraft:block.fire.extinguish", soundBlocks, 0.5, 2.6+(w.levelRandom().NextFloat()-w.levelRandom().NextFloat())*0.8)
			return true
		}
		st = world.FluidState(world.FluidWater, 0)
	case "minecraft:lava_bucket":
		st, snd = world.FluidState(world.FluidLava, 0), "minecraft:item.bucket.empty_lava"
	case "minecraft:powder_snow_bucket":
		st, _ = data.DefaultState("powder_snow")
		snd = "minecraft:item.bucket.empty_powder_snow"
		if !data.IsAir(cur) && data.Flags(cur)&data.FlagLiquid == 0 && !world.FluidBreaks(cur) && data.Flags(cur)&data.FlagReplaceable == 0 {
			return false
		}
	default:
		return false
	}
	if !data.IsAir(cur) && data.Flags(cur)&data.FlagLiquid == 0 {
		w.dropBlockNoTool(pos, cur)
	}
	w.w.SetBlock(pos.X, pos.Y, pos.Z, st)
	w.playSoundAt(pos.Center(), snd, soundBlocks, 1, 1)
	return true
}

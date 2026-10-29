package feature

import "github.com/levitskyyy/KremenMC/internal/nbt"

// beRegion is implemented by regions that can store block entities (the
// pipeline's StageContext); test regions without it drop them.
type beRegion interface {
	SetBlockEntity(x, y, z int, id string, d nbt.Compound) bool
}

// SetBlockEntity stores block entity NBT (without id/x/y/z) at p, replacing
// any existing one. The block must already be set.
func (lv *Level) SetBlockEntity(p Pos, id string, d nbt.Compound) bool {
	if d == nil {
		d = nbt.Compound{}
	}
	if _, ok := d["components"]; !ok {
		d["components"] = nbt.Compound{} // every saved vanilla block entity has it
	}
	if b, ok := lv.rg.(beRegion); ok {
		return b.SetBlockEntity(p.X, p.Y, p.Z, id, d)
	}
	return false
}

package carver

import "github.com/levitskyyy/KremenMC/internal/data"

// Aquifer decides what a carved position becomes (reference
// Aquifer.computeSubstance called with density 0): ok=false leaves the
// block in place (aquifer barrier).
type Aquifer interface {
	Substance(x, y, z int) (st data.StateID, ok bool)
}

// Blocks is the writable chunk the mask is applied to (local x,z).
type Blocks interface {
	SetBlock(x, y, z int, s data.StateID) data.StateID
}

// TopMaterial re-tops dirt uncovered by carving (surface.System.TopMaterial
// at absolute coordinates); ok=false keeps the dirt.
type TopMaterial func(x, y, z int, underFluid bool) (data.StateID, bool)

// Apply replaces the carved positions of chunk cx,cz with the aquifer's
// substance, visiting columns x-major, then z, and each run of carved
// positions top-down from the lowest run up, as the reference does.
func (m *Mask) Apply(cx, cz int32, aq Aquifer, dst Blocks) { m.ApplyTop(cx, cz, aq, dst, nil) }

// ApplyTop is Apply plus the reference grass fix: once a column has carved
// a grass block or mycelium, dirt directly below each later carved position
// gets retop (when dst can be read and retop is not nil).
func (m *Mask) ApplyTop(cx, cz int32, aq Aquifer, dst Blocks, retop TopMaterial) {
	rd, _ := dst.(interface {
		Block(x, y, z int) data.StateID
	})
	if rd == nil {
		retop = nil
	}
	reg := data.Canonical()
	grass, _ := reg.DefaultState("minecraft:grass_block")
	myc, _ := reg.DefaultState("minecraft:mycelium")
	dirt, _ := reg.DefaultState("minecraft:dirt")
	bx, bz := int(cx)<<4, int(cz)<<4
	for x := range 16 {
		for z := range 16 {
			surface := false
			y := m.minY
			top := m.minY + m.height
			for y < top {
				if !m.Get(x, y, z) {
					y++
					continue
				}
				y0 := y
				for y < top && m.Get(x, y, z) {
					y++
				}
				for py := y - 1; py >= y0; py-- {
					st, ok := aq.Substance(bx+x, py, bz+z)
					if !ok {
						continue
					}
					if retop != nil {
						if o := reg.BlockID(rd.Block(x, py, z)); o == reg.BlockID(grass) || o == reg.BlockID(myc) {
							surface = true
						}
					}
					dst.SetBlock(x, py, z, st)
					if surface && py-1 >= m.minY && rd.Block(x, py-1, z) == dirt {
						if t, ok := retop(bx+x, py-1, bz+z, reg.HasFluid(st)); ok {
							dst.SetBlock(x, py-1, z, t)
						}
					}
				}
			}
		}
	}
}

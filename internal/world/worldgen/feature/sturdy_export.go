package feature

import "github.com/levitskyyy/KremenMC/internal/data"

// FaceSturdy reports BlockState.isFaceSturdy (full support) of st on face
// dir ("down", "up", "north", "south", "west", "east"), from the reference
// table predicate_sturdy.txt.
func FaceSturdy(st data.StateID, dir string) bool {
	for i, d := range dirOrder {
		if d == dir {
			return faceSturdy(st, i)
		}
	}
	return false
}

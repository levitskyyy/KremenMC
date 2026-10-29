package structure

import "github.com/levitskyyy/KremenMC/internal/data"

func BlockAgeForTest(s data.StateID, m float32, x, y, z int32) data.StateID {
	return blockAge(data.Canonical(), s, m, x, y, z)
}

func BlackstoneForTest(s data.StateID) data.StateID { return blackstoneReplace(data.Canonical(), s) }

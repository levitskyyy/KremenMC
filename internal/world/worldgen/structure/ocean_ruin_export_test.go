package structure

// SetOceanRuinDebug logs cluster decisions (tests).
func SetOceanRuinDebug(f func(string)) { oceanRuinDebug = f }

// OceanRuinPiecesForTest returns the pieces of one ruin at x, 90, z.
func OceanRuinPiecesForTest(warm bool, x, z int32, rot Rotation, big bool, n int32) ([]Piece, error) {
	bt := "COLD"
	if warm {
		bt = "WARM"
	}
	return oceanRuinType{warm: warm, biomeTemp: bt}.ruin(x, z, rot, big, n)
}

// SetOceanRuinYHook observes the height of every ocean ruin placement.
func SetOceanRuinYHook(f func(y int32)) { oceanRuinYHook = f }

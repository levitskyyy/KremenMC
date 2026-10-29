package surface

// ColdEnoughToSnow reports whether biome key is cold enough to snow at
// x,y,z (height-adjusted temperature < 0.15), for structure starts.
func ColdEnoughToSnow(reg Registry, key string, x, y, z, seaLevel int32) (bool, error) {
	c, err := loadClimate(reg, key)
	if err != nil {
		return false, err
	}
	return c.coldEnoughToSnow(x, y, z, seaLevel), nil
}

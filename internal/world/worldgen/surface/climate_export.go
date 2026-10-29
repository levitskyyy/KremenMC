package surface

// BiomeClimate is the temperature part of a biome's climate settings.
type BiomeClimate struct{ c climate }

// LoadBiomeClimate reads the climate of the biome key from reg.
func LoadBiomeClimate(reg Registry, key string) (BiomeClimate, error) {
	c, err := loadClimate(reg, key)
	return BiomeClimate{c}, err
}

// ColdEnoughToSnow is the reference Biome.coldEnoughToSnow (height-adjusted
// temperature below 0.15).
func (b BiomeClimate) ColdEnoughToSnow(x, y, z, seaLevel int32) bool {
	return b.c.coldEnoughToSnow(x, y, z, seaLevel)
}

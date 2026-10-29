package surface

// ZoomQuart is the quart cell BiomeManager.getBiome reads for block x,y,z
// with an obfuscated seed (ObfuscateSeed).
func ZoomQuart(seed int64, x, y, z int32) (int32, int32, int32) { return zoomQuart(seed, x, y, z) }

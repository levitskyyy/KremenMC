package structure

// refOrder is the order in which a chunk places the starts it references:
// the iteration order of an open-addressing long hash set of capacity 32
// (64, 128, ... past 24, 48, ... entries), linear probing, keys
// x | z<<32, slot = mix(key) & (cap-1) with the 64-bit golden-ratio mix,
// visited from the last slot down. Inferred from the reference worlds:
// it reproduces 219/244 saved References arrays (z-ascending: 188) and the
// placement order of all 4 two-start mineshaft chunks found (shared
// placement random, checked block by block). Collisions are resolved in
// kremenmc's insertion order (x-major), which vanilla may not share.
func refOrder(refs []int64) []int64 {
	if len(refs) < 2 {
		return refs
	}
	capacity := 32
	for len(refs) > capacity*3/4 {
		capacity *= 2
	}
	table := make([]int64, capacity)
	used := make([]bool, capacity)
	for _, k := range refs {
		x, z := int32(uint64(k)>>32), int32(uint32(k))
		key := uint64(uint32(x)) | uint64(uint32(z))<<32
		h := key * 0x9E3779B97F4A7C15
		h ^= h >> 32
		h ^= h >> 16
		s := int(h & uint64(capacity-1))
		for used[s] {
			s = (s + 1) & (capacity - 1)
		}
		table[s], used[s] = k, true
	}
	out := make([]int64, 0, len(refs))
	for s := capacity - 1; s >= 0; s-- {
		if used[s] {
			out = append(out, table[s])
		}
	}
	return out
}

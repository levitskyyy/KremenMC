package structure

// SetStrongholdBoundsForTest sets the layout bounds and returns the old
// values.
func SetStrongholdBoundsForTest(depth, minY, fillerMinY int32) (int32, int32, int32) {
	d, m, f := shMaxDepth, shMinY, shFillerMinY
	shMaxDepth, shMinY, shFillerMinY = depth, minY, fillerMinY
	return d, m, f
}

// DecoIndexForTest is a structure's decoration step and index.
func DecoIndexForTest(key string) [2]int32 { return decoIndex()[key] }

// SetShTrace sets the stronghold placement draw trace.
func SetShTrace(f func(what string, v any)) { shTrace = f }

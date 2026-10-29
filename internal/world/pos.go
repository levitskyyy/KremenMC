package world

type ChunkPos struct{ X, Z int32 }

func (p ChunkPos) Key() uint64 { return uint64(uint32(p.X))<<32 | uint64(uint32(p.Z)) }

func PosFromKey(k uint64) ChunkPos { return ChunkPos{int32(k >> 32), int32(uint32(k))} }

// Region returns the 8x8-chunk region containing p.
func (p ChunkPos) Region() RegionPos { return RegionPos{p.X >> RegionShift, p.Z >> RegionShift} }

func ChunkAt(x, z int) ChunkPos { return ChunkPos{int32(x >> 4), int32(z >> 4)} }

const (
	RegionShift = 3
	RegionSize  = 1 << RegionShift
)

type RegionPos struct{ X, Z int32 }

func (r RegionPos) Key() uint64 { return uint64(uint32(r.X))<<32 | uint64(uint32(r.Z)) }

// Color is the checkerboard phase (0..3); same-colored regions never touch.
func (r RegionPos) Color() int { return int(r.X&1) | int(r.Z&1)<<1 }

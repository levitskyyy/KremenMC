package density

// cornerTable is an open-addressing hash map from (interpolator, corner) to
// its value. Reset is O(1) via a generation stamp, so the table is reused
// across chunks without clearing or reallocating.
type cornerTable struct {
	slots []cornerSlot
	mask  uint32
	n     int
	gen   uint32
	// last memoizes each interpolator's most recent cell corners.
	last []lastCell
}

type lastCell struct {
	x, y, z int32
	gen     uint32
	v       [8]float32
}

type cornerSlot struct {
	id, x, y, z int32
	gen         uint32
	v           float32
}

func newCornerTable() *cornerTable {
	const size = 1 << 14
	return &cornerTable{slots: make([]cornerSlot, size), mask: size - 1, gen: 1}
}

func (t *cornerTable) reset() {
	t.n = 0
	t.gen++
	if t.gen == 0 {
		clear(t.slots)
		clear(t.last)
		t.gen = 1
	}
}

func cornerHash(id, x, y, z int32) uint32 {
	h := uint64(uint32(x))*0x9E3779B97F4A7C15 ^ uint64(uint32(z))*0xC2B2AE3D27D4EB4F ^
		uint64(uint32(y))*0x165667B19E3779F9 ^ uint64(uint32(id))*0x27D4EB2F165667C5
	h ^= h >> 29
	h *= 0xBF58476D1CE4E5B9
	return uint32(h >> 32)
}

// find returns the slot holding the key, or the empty slot to insert it at.
func (t *cornerTable) find(id, x, y, z int32) (uint32, bool) {
	i := cornerHash(id, x, y, z) & t.mask
	for {
		s := &t.slots[i]
		if s.gen != t.gen {
			return i, false
		}
		if s.x == x && s.z == z && s.y == y && s.id == id {
			return i, true
		}
		i = (i + 1) & t.mask
	}
}

// insert looks the slot up again: computing v may have inserted nested
// interpolators' corners and grown the table.
func (t *cornerTable) insert(id, x, y, z int32, v float32) {
	i, ok := t.find(id, x, y, z)
	if ok {
		t.slots[i].v = v
		return
	}
	t.slots[i] = cornerSlot{id, x, y, z, t.gen, v}
	t.n++
	if t.n*2 > len(t.slots) {
		t.grow()
	}
}

func (t *cornerTable) grow() {
	old := t.slots
	t.slots = make([]cornerSlot, len(old)*2)
	t.mask = uint32(len(t.slots) - 1)
	for _, s := range old {
		if s.gen == t.gen {
			i, _ := t.find(s.id, s.x, s.y, s.z)
			t.slots[i] = s
		}
	}
}

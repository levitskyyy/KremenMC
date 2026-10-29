package carver

// Output receives carved positions: x and z local to the target chunk
// (0..15), y absolute (reference CarverOutput).
type Output interface {
	Carve(x, y, z int)
	MinY() int
	MaxY() int
}

// Mask is the reference CarvingMask: one bit per position of a chunk
// between MinY and MaxY inclusive.
type Mask struct {
	minY, height int
	bits         []uint64
}

func NewMask(minY, height int) *Mask {
	return &Mask{minY: minY, height: height, bits: make([]uint64, (256*height+63)/64)}
}

func (m *Mask) MinY() int { return m.minY }
func (m *Mask) MaxY() int { return m.minY + m.height - 1 }

func (m *Mask) index(x, y, z int) int { return x&15 | (z&15)<<4 | (y-m.minY)<<8 }

func (m *Mask) Carve(x, y, z int) {
	if y < m.minY || y >= m.minY+m.height {
		return
	}
	i := m.index(x, y, z)
	m.bits[i>>6] |= 1 << (i & 63)
}

// Get reports whether local x,z and absolute y are carved.
func (m *Mask) Get(x, y, z int) bool {
	if y < m.minY || y >= m.minY+m.height {
		return false
	}
	i := m.index(x, y, z)
	return m.bits[i>>6]&(1<<(i&63)) != 0
}

// Reset clears the mask for reuse.
func (m *Mask) Reset() { clear(m.bits) }

// Empty reports whether nothing is carved.
func (m *Mask) Empty() bool {
	for _, w := range m.bits {
		if w != 0 {
			return false
		}
	}
	return true
}

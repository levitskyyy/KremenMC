package world

import (
	"math/bits"

	"github.com/levitskyyy/KremenMC/internal/proto"
)

// Strategy defines palette thresholds for a container kind. The values match
// what the vanilla client derives from a received bits-per-entry byte.
type Strategy struct {
	Size       int   // entries: 4096 blocks, 64 biomes
	MinBits    uint8 // smallest indirect width (blocks round 1..3 up to 4)
	MaxBits    uint8 // largest indirect width
	GlobalBits uint8 // direct width: ceil(log2(registry size))
}

func NewStrategy(size int, minBits, maxBits uint8, registrySize int) *Strategy {
	return &Strategy{Size: size, MinBits: minBits, MaxBits: maxBits, GlobalBits: GlobalBits(registrySize)}
}

func GlobalBits(registrySize int) uint8 {
	if registrySize <= 1 {
		return 1
	}
	return uint8(bits.Len(uint(registrySize - 1)))
}

// bitsFor returns the storage width for a palette holding n distinct values.
func (s *Strategy) bitsFor(n int) uint8 {
	if n <= 1 {
		return 0
	}
	b := uint8(bits.Len(uint(n - 1)))
	if b < s.MinBits {
		b = s.MinBits
	}
	if b > s.MaxBits {
		return s.GlobalBits
	}
	return b
}

func (s *Strategy) direct(b uint8) bool { return b > s.MaxBits }

// Container is a paletted array of Size entries. Not safe for concurrent mutation.
type Container struct {
	s       *Strategy
	bits    uint8
	perLong uint8
	mask    uint64
	palette []uint32 // single: 1 entry; indirect: local->global; direct: nil
	index   map[uint32]uint32
	data    []uint64
}

func NewContainer(s *Strategy, fill uint32) Container {
	return Container{s: s, palette: []uint32{fill}}
}

func (c *Container) Strategy() *Strategy { return c.s }
func (c *Container) Bits() uint8         { return c.bits }

// Palette returns the local palette (nil when direct). Do not modify.
func (c *Container) Palette() []uint32 { return c.palette }

// Single reports whether every entry holds the same value.
func (c *Container) Single() (uint32, bool) {
	if c.bits == 0 {
		return c.palette[0], true
	}
	return 0, false
}

func (c *Container) Get(i int) uint32 {
	if c.bits == 0 {
		return c.palette[0]
	}
	pl := int(c.perLong)
	v := uint32(c.data[i/pl]>>(uint(i%pl)*uint(c.bits))) & uint32(c.mask)
	if c.palette == nil {
		return v
	}
	return c.palette[v]
}

func (c *Container) raw(i int) uint32 {
	pl := int(c.perLong)
	return uint32(c.data[i/pl]>>(uint(i%pl)*uint(c.bits))) & uint32(c.mask)
}

func (c *Container) setRaw(i int, v uint32) {
	pl := int(c.perLong)
	shift := uint(i%pl) * uint(c.bits)
	w := &c.data[i/pl]
	*w = *w&^(c.mask<<shift) | uint64(v)<<shift
}

// Set stores v at i and returns the previous value.
func (c *Container) Set(i int, v uint32) uint32 {
	if c.bits == 0 {
		old := c.palette[0]
		if old == v {
			return old
		}
		c.resize(c.s.bitsFor(2))
	}
	if c.palette == nil {
		old := c.raw(i)
		c.setRaw(i, v)
		return old
	}
	old := c.palette[c.raw(i)]
	if old == v {
		return old
	}
	idx, ok := c.lookup(v)
	if !ok {
		if len(c.palette) == 1<<c.bits {
			c.resize(c.s.bitsFor(len(c.palette) + 1))
			if c.palette == nil {
				c.setRaw(i, v)
				return old
			}
		}
		idx = uint32(len(c.palette))
		c.palette = append(c.palette, v)
		if c.index != nil {
			c.index[v] = idx
		} else if len(c.palette) > 16 {
			c.buildIndex()
		}
	}
	c.setRaw(i, idx)
	return old
}

func (c *Container) lookup(v uint32) (uint32, bool) {
	if c.index != nil {
		i, ok := c.index[v]
		return i, ok
	}
	for i, p := range c.palette {
		if p == v {
			return uint32(i), true
		}
	}
	return 0, false
}

func (c *Container) buildIndex() {
	c.index = make(map[uint32]uint32, len(c.palette)*2)
	for i, p := range c.palette {
		c.index[p] = uint32(i)
	}
}

func (c *Container) setBits(b uint8) {
	c.bits = b
	if b == 0 {
		c.perLong, c.mask, c.data = 0, 0, nil
		return
	}
	c.perLong = 64 / b
	c.mask = 1<<b - 1
	c.data = make([]uint64, longsFor(c.s.Size, b))
}

func (c *Container) resize(nb uint8) {
	old := *c
	c.setBits(nb)
	c.index = nil
	if c.s.direct(nb) {
		c.palette = nil
		for i := 0; i < c.s.Size; i++ {
			c.setRaw(i, old.Get(i))
		}
		return
	}
	if old.bits == 0 {
		c.palette = append(make([]uint32, 0, 1<<nb), old.palette[0])
		return
	}
	// indirect -> wider indirect keeps indices
	c.palette = append(make([]uint32, 0, 1<<nb), old.palette...)
	if len(c.palette) > 16 {
		c.buildIndex()
	}
	for i := 0; i < c.s.Size; i++ {
		c.setRaw(i, old.raw(i))
	}
}

// Fill sets every entry to v.
func (c *Container) Fill(v uint32) {
	c.setBits(0)
	c.index = nil
	c.palette = append(c.palette[:0:0], v)
}

// SetAll replaces the contents with vals (len == Size), choosing the smallest palette.
func (c *Container) SetAll(vals []uint32) {
	vals = vals[:c.s.Size]
	first := vals[0]
	uniform := true
	for _, v := range vals {
		if v != first {
			uniform = false
			break
		}
	}
	if uniform {
		c.Fill(first)
		return
	}
	pal := make([]uint32, 0, 16)
	var idx map[uint32]uint32
	local := make([]uint32, len(vals))
	for i, v := range vals {
		var li uint32
		found := false
		if idx != nil {
			li, found = idx[v]
		} else {
			for j, p := range pal {
				if p == v {
					li, found = uint32(j), true
					break
				}
			}
		}
		if !found {
			li = uint32(len(pal))
			pal = append(pal, v)
			if idx != nil {
				idx[v] = li
			} else if len(pal) > 16 {
				idx = make(map[uint32]uint32, 64)
				for j, p := range pal {
					idx[p] = uint32(j)
				}
			}
		}
		local[i] = li
	}
	nb := c.s.bitsFor(len(pal))
	c.setBits(nb)
	if c.s.direct(nb) {
		c.palette, c.index = nil, nil
		for i, v := range vals {
			c.setRaw(i, v)
		}
		return
	}
	c.palette, c.index = pal, idx
	for i, li := range local {
		c.setRaw(i, li)
	}
}

// Values expands the container into dst (len >= Size).
func (c *Container) Values(dst []uint32) []uint32 {
	dst = dst[:c.s.Size]
	if c.bits == 0 {
		for i := range dst {
			dst[i] = c.palette[0]
		}
		return dst
	}
	for i := range dst {
		dst[i] = c.Get(i)
	}
	return dst
}

// Compact drops unused palette entries and shrinks the width if possible.
func (c *Container) Compact() {
	if c.bits == 0 {
		return
	}
	vals := make([]uint32, c.s.Size)
	c.SetAll(c.Values(vals))
}

// Clone returns a deep copy.
func (c *Container) Clone() Container {
	n := *c
	n.palette = append([]uint32(nil), c.palette...)
	n.data = append([]uint64(nil), c.data...)
	if c.index != nil {
		n.buildIndex()
	}
	return n
}

// Count calls fn for each distinct value with its occurrence count.
func (c *Container) Count(fn func(v uint32, n int)) {
	if c.bits == 0 {
		fn(c.palette[0], c.s.Size)
		return
	}
	if c.palette == nil {
		for i := 0; i < c.s.Size; i++ {
			fn(c.raw(i), 1)
		}
		return
	}
	var small [256]int32
	for i := 0; i < c.s.Size; i++ {
		small[c.raw(i)]++
	}
	for i, n := range small[:len(c.palette)] {
		if n > 0 {
			fn(c.palette[i], int(n))
		}
	}
}

func longsFor(n int, b uint8) int {
	if b == 0 {
		return 0
	}
	pl := 64 / int(b)
	return (n + pl - 1) / pl
}

// NetSize is the encoded size of the container in the chunk data packet.
func (c *Container) NetSize() int {
	n := 1
	switch {
	case c.bits == 0:
		n += proto.VarIntSize(int32(c.palette[0]))
	case c.palette != nil:
		n += proto.VarIntSize(int32(len(c.palette)))
		for _, p := range c.palette {
			n += proto.VarIntSize(int32(p))
		}
	}
	return n + 8*len(c.data)
}

// Encode writes the 26.x network form: u8 bits, palette, then the long array
// without a length prefix (removed in 1.21.5).
func (c *Container) Encode(w *proto.Writer) {
	w.U8(c.bits)
	switch {
	case c.bits == 0:
		w.VarInt(int32(c.palette[0]))
	case c.palette != nil:
		w.VarInt(int32(len(c.palette)))
		for _, p := range c.palette {
			w.VarInt(int32(p))
		}
	}
	for _, l := range c.data {
		w.U64(l)
	}
}

// Decode reads the network form produced by Encode.
func (c *Container) Decode(r *proto.Reader) error {
	b, err := r.U8()
	if err != nil {
		return err
	}
	c.index = nil
	switch {
	case b == 0:
		v, err := r.VarInt()
		if err != nil {
			return err
		}
		c.Fill(uint32(v))
		return nil
	case b <= c.s.MaxBits:
		if b < c.s.MinBits {
			b = c.s.MinBits
		}
		n, err := r.VarInt()
		if err != nil {
			return err
		}
		if n < 1 || int(n) > 1<<b {
			return errBadPalette
		}
		pal := make([]uint32, n)
		for i := range pal {
			v, err := r.VarInt()
			if err != nil {
				return err
			}
			pal[i] = uint32(v)
		}
		c.setBits(b)
		c.palette = pal
		if len(pal) > 16 {
			c.buildIndex()
		}
	default:
		c.setBits(c.s.GlobalBits)
		c.palette = nil
	}
	for i := range c.data {
		v, err := r.I64()
		if err != nil {
			return err
		}
		c.data[i] = uint64(v)
	}
	if c.palette != nil {
		for i := 0; i < c.s.Size; i++ {
			if int(c.raw(i)) >= len(c.palette) {
				return errBadPalette
			}
		}
	}
	return nil
}

// PackIndices packs vals with the given width using the no-span long layout.
func PackIndices(dst []uint64, vals []uint32, b uint8) []uint64 {
	n := longsFor(len(vals), b)
	dst = append(dst[:0], make([]uint64, n)...)
	pl := 64 / int(b)
	for i, v := range vals {
		dst[i/pl] |= uint64(v) << (uint(i%pl) * uint(b))
	}
	return dst
}

// UnpackIndices is the inverse of PackIndices. It fails if data is too short.
func UnpackIndices(dst []uint32, data []uint64, b uint8) bool {
	if b == 0 {
		clear(dst)
		return true
	}
	if len(data) < longsFor(len(dst), b) {
		return false
	}
	pl := 64 / int(b)
	mask := uint64(1)<<b - 1
	for i := range dst {
		dst[i] = uint32(data[i/pl] >> (uint(i%pl) * uint(b)) & mask)
	}
	return true
}

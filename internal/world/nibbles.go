package world

// Nibbles is a 16x16x16 array of 4-bit light values (index y<<8|z<<4|x, low
// nibble first). The zero value reads as all 0; FullNibbles reads as all 15.
// Both share storage until written.
type Nibbles struct{ p *[2048]byte }

var fullArr = func() *[2048]byte {
	var a [2048]byte
	for i := range a {
		a[i] = 0xFF
	}
	return &a
}()

func FullNibbles() Nibbles { return Nibbles{fullArr} }

// NibblesFrom copies a 2048-byte array; it returns false for other lengths.
func NibblesFrom(b []byte) (Nibbles, bool) {
	if len(b) != 2048 {
		return Nibbles{}, false
	}
	var a [2048]byte
	copy(a[:], b)
	return Nibbles{&a}, true
}

func (n Nibbles) IsZero() bool {
	if n.p == nil {
		return true
	}
	if n.p == fullArr {
		return false
	}
	for _, b := range n.p {
		if b != 0 {
			return false
		}
	}
	return true
}

func (n Nibbles) IsFull() bool { return n.p == fullArr }

// Bytes returns the backing array, or nil for an all-zero value. Do not modify.
func (n Nibbles) Bytes() []byte {
	if n.p == nil {
		return nil
	}
	return n.p[:]
}

func (n Nibbles) Get(i int) uint8 {
	if n.p == nil {
		return 0
	}
	return n.p[i>>1] >> (uint(i&1) << 2) & 15
}

func (n *Nibbles) Set(i int, v uint8) {
	if n.p == nil {
		if v == 0 {
			return
		}
		n.p = new([2048]byte)
	} else if n.p == fullArr {
		if v == 15 {
			return
		}
		a := *fullArr
		n.p = &a
	}
	sh := uint(i&1) << 2
	b := &n.p[i>>1]
	*b = *b&^(15<<sh) | v<<sh
}

func (n Nibbles) Clone() Nibbles {
	if n.p == nil || n.p == fullArr {
		return n
	}
	a := *n.p
	return Nibbles{&a}
}

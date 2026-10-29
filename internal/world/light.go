package world

import "github.com/levitskyyy/KremenMC/internal/data"

// ChunkSource resolves loaded chunks by chunk coordinates (nil if absent).
type ChunkSource func(cx, cz int32) *Chunk

type lightNode struct {
	x, y, z int32
	l       uint8
}

type lightQueue struct {
	q    []lightNode
	head int
}

func (q *lightQueue) push(x, y, z int32, l uint8) { q.q = append(q.q, lightNode{x, y, z, l}) }

func (q *lightQueue) pop() (lightNode, bool) {
	if q.head == len(q.q) {
		q.q, q.head = q.q[:0], 0
		return lightNode{}, false
	}
	n := q.q[q.head]
	q.head++
	return n, true
}

var dirs = [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

const dirDown = 0

// LightEngine propagates sky and block light. One engine per goroutine; it
// write-locks every chunk it touches for the duration of an operation, so
// operations from different regions must touch disjoint chunks (the
// checkerboard schedule guarantees this for a 1-chunk margin).
type LightEngine struct {
	src     ChunkSource
	dim     Dimension
	inc     lightQueue
	dec     lightQueue
	locked  []*Chunk
	noLock  bool
	lastKey uint64
	last    *Chunk
	haveLst bool
}

func NewLightEngine(dim Dimension, src ChunkSource) *LightEngine {
	return &LightEngine{src: src, dim: dim}
}

func (e *LightEngine) chunk(x, z int32) *Chunk {
	k := ChunkPos{x >> 4, z >> 4}.Key()
	if e.haveLst && k == e.lastKey {
		return e.last
	}
	c := e.src(x>>4, z>>4)
	if c != nil && !e.noLock {
		found := false
		for _, l := range e.locked {
			if l == c {
				found = true
				break
			}
		}
		if !found {
			c.mu.Lock()
			e.locked = append(e.locked, c)
		}
	}
	e.last, e.lastKey, e.haveLst = c, k, true
	return c
}

func (e *LightEngine) release() {
	for _, c := range e.locked {
		c.mu.Unlock()
	}
	e.locked = e.locked[:0]
	e.haveLst, e.last = false, nil
}

func (e *LightEngine) inWorld(y int32) bool {
	return int(y) >= e.dim.MinY && int(y) < e.dim.MinY+e.dim.Height
}

// state and light accessors; ok is false when the chunk is not loaded
func (e *LightEngine) get(sky bool, x, y, z int32) (data.StateID, uint8, bool) {
	c := e.chunk(x, z)
	if c == nil {
		return 0, 0, false
	}
	si := (int(y) - c.Dim.MinY) >> 4
	li := int(y&15)<<8 | int(z&15)<<4 | int(x&15)
	st := data.StateID(c.Sections[si].Blocks.Get(li))
	if sky {
		return st, c.SkyLight[si+1].Get(li), true
	}
	return st, c.BlockLight[si+1].Get(li), true
}

func (e *LightEngine) set(sky bool, x, y, z int32, l uint8) {
	c := e.chunk(x, z)
	if c == nil {
		return
	}
	si := (int(y)-c.Dim.MinY)>>4 + 1
	li := int(y&15)<<8 | int(z&15)<<4 | int(x&15)
	if sky {
		c.SkyLight[si].Set(li, l)
	} else {
		c.BlockLight[si].Set(li, l)
	}
}

// step returns the light level entering a block with filter f from level l.
func step(sky bool, dir int, l, f uint8) uint8 {
	if sky && dir == dirDown && l == 15 && f == 0 {
		return 15
	}
	d := max(f, 1)
	if l <= d {
		return 0
	}
	return l - d
}

func (e *LightEngine) propagate(sky bool) {
	for {
		n, ok := e.inc.pop()
		if !ok {
			return
		}
		_, cur, ok := e.get(sky, n.x, n.y, n.z)
		if !ok || cur != n.l || n.l <= 1 {
			continue
		}
		for d, dv := range dirs {
			x, y, z := n.x+dv[0], n.y+dv[1], n.z+dv[2]
			if !e.inWorld(y) {
				continue
			}
			st, l, ok := e.get(sky, x, y, z)
			if !ok {
				continue
			}
			nl := step(sky, d, n.l, data.LightFilter(st))
			if nl > l {
				e.set(sky, x, y, z, nl)
				e.inc.push(x, y, z, nl)
			}
		}
	}
}

func (e *LightEngine) unpropagate(sky bool) {
	for {
		n, ok := e.dec.pop()
		if !ok {
			return
		}
		for d, dv := range dirs {
			x, y, z := n.x+dv[0], n.y+dv[1], n.z+dv[2]
			if !e.inWorld(y) {
				continue
			}
			st, l, ok := e.get(sky, x, y, z)
			if !ok || l == 0 {
				continue
			}
			if l < n.l || (sky && d == dirDown && n.l == 15 && l == 15) {
				e.set(sky, x, y, z, 0)
				e.dec.push(x, y, z, l)
				var re uint8
				if sky {
					if int(y) == e.dim.MinY+e.dim.Height-1 {
						re = step(true, dirDown, 15, data.LightFilter(st))
					}
				} else {
					re = data.LightEmission(st)
				}
				if re > 0 {
					e.set(sky, x, y, z, re)
					e.inc.push(x, y, z, re)
				}
			} else {
				e.inc.push(x, y, z, l)
			}
		}
	}
}

// Update relights after the block at x,y,z changed. Callers must not hold
// any chunk lock.
func (e *LightEngine) Update(x, y, z int) {
	if !e.inWorld(int32(y)) {
		return
	}
	defer e.release()
	X, Y, Z := int32(x), int32(y), int32(z)
	if e.dim.HasSkylight {
		e.update(true, X, Y, Z)
	}
	e.update(false, X, Y, Z)
}

func (e *LightEngine) update(sky bool, x, y, z int32) {
	st, old, ok := e.get(sky, x, y, z)
	if !ok {
		return
	}
	if old > 0 {
		e.set(sky, x, y, z, 0)
		e.dec.push(x, y, z, old)
		e.unpropagate(sky)
	}
	if !sky {
		if em := data.LightEmission(st); em > 0 {
			if _, cur, _ := e.get(false, x, y, z); em > cur {
				e.set(false, x, y, z, em)
				e.inc.push(x, y, z, em)
			}
		}
	}
	e.seedNeighbors(sky, x, y, z)
	e.propagate(sky)
}

func (e *LightEngine) seedNeighbors(sky bool, x, y, z int32) {
	for _, dv := range dirs {
		nx, ny, nz := x+dv[0], y+dv[1], z+dv[2]
		if !e.inWorld(ny) {
			if sky && ny >= int32(e.dim.MinY+e.dim.Height) {
				// open sky above the world
				st, l, ok := e.get(true, x, y, z)
				if ok {
					if nl := step(true, dirDown, 15, data.LightFilter(st)); nl > l {
						e.set(true, x, y, z, nl)
						e.inc.push(x, y, z, nl)
					}
				}
			}
			continue
		}
		if _, l, ok := e.get(sky, nx, ny, nz); ok && l > 1 {
			e.inc.push(nx, ny, nz, l)
		}
	}
}

// LightChunk computes light for c in isolation (no neighbor access, no
// locking): use it on chunks that are not yet shared, then Stitch.
func (e *LightEngine) LightChunk(c *Chunk) {
	src, dim := e.src, e.dim
	e.src = func(cx, cz int32) *Chunk {
		if cx == c.X && cz == c.Z {
			return c
		}
		return nil
	}
	e.dim, e.noLock = c.Dim, true
	defer func() {
		e.src, e.dim, e.noLock = src, dim, false
		e.haveLst, e.last = false, nil
	}()

	n := len(c.Sections)
	for i := range c.SkyLight {
		c.SkyLight[i], c.BlockLight[i] = Nibbles{}, Nibbles{}
	}
	bx, bz := c.X<<4, c.Z<<4
	if c.Dim.HasSkylight {
		e.skyColumns(c)
		c.SkyLight[n+1] = FullNibbles()
		e.propagate(true)
	}
	for si := range c.Sections {
		s := &c.Sections[si]
		if s.Empty() {
			continue
		}
		emits := false
		if s.Blocks.Palette() == nil {
			emits = true
		} else {
			for _, v := range s.Blocks.Palette() {
				if data.LightEmission(data.StateID(v)) > 0 {
					emits = true
					break
				}
			}
		}
		if !emits {
			continue
		}
		y0 := int32(c.Dim.MinY + si<<4)
		for i := range 4096 {
			if em := data.LightEmission(data.StateID(s.Blocks.Get(i))); em > 0 {
				x, y, z := bx+int32(i&15), y0+int32(i>>8), bz+int32(i>>4&15)
				c.BlockLight[si+1].Set(i, em)
				e.inc.push(x, y, z, em)
			}
		}
	}
	e.propagate(false)
	c.LightReady = true
}

// skyColumns does the vertical sky pass and queues horizontal spread seeds.
func (e *LightEngine) skyColumns(c *Chunk) {
	minY, top := c.Dim.MinY, c.Dim.MinY+c.Dim.Height
	var open [256]int // lowest y still at 15 per column
	maxH := minY
	for col := range 256 {
		h := c.Height(WorldSurface, col&15, col>>4)
		maxH = max(maxH, h)
		open[col] = h
	}
	fullFrom := minY + (maxH-minY+15)&^15
	for si := range c.Sections {
		if minY+si<<4 >= fullFrom {
			c.SkyLight[si+1] = FullNibbles()
		}
	}
	bx, bz := c.X<<4, c.Z<<4
	var lowLit [256]int
	for col := range 256 {
		for y := open[col]; y < fullFrom; y++ {
			si := (y - minY) >> 4
			c.SkyLight[si+1].Set((y&15)<<8|col, 15)
		}
		l := uint8(15)
		y := open[col] - 1
		for ; y >= minY; y-- {
			si := (y - minY) >> 4
			idx := (y&15)<<8 | col
			nl := step(true, dirDown, l, data.LightFilter(data.StateID(c.Sections[si].Blocks.Get(idx))))
			if nl == 0 {
				break
			}
			if nl == 15 {
				open[col] = y
			}
			l = nl
			c.SkyLight[si+1].Set(idx, l)
		}
		lowLit[col] = y + 1
	}
	for col := range 256 {
		x, z := col&15, col>>4
		hi := open[col]
		for _, d := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nx, nz := x+d[0], z+d[1]
			if nx < 0 || nx > 15 || nz < 0 || nz > 15 {
				continue
			}
			hi = max(hi, open[nz<<4|nx])
		}
		for y := lowLit[col]; y < hi && y < top; y++ {
			si := (y - minY) >> 4
			if l := c.SkyLight[si+1].Get((y&15)<<8 | col); l > 1 {
				e.inc.push(bx+int32(x), int32(y), bz+int32(z), l)
			}
		}
	}
}

// Stitch exchanges light across the borders of chunk (cx,cz) and its loaded
// horizontal neighbors. Call after a chunk becomes visible to the source.
func (e *LightEngine) Stitch(cx, cz int32) {
	c := e.src(cx, cz)
	if c == nil {
		return
	}
	defer e.release()
	for _, sky := range [2]bool{true, false} {
		if sky && !e.dim.HasSkylight {
			continue
		}
		for _, d := range [4][2]int32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			nb := e.src(cx+d[0], cz+d[1])
			if nb == nil {
				continue
			}
			e.seedBorder(sky, c, nb, d[0], d[1])
			e.seedBorder(sky, nb, c, -d[0], -d[1])
		}
		e.propagate(sky)
	}
}

// seedBorder queues border cells of c that can raise the facing cell of nb.
func (e *LightEngine) seedBorder(sky bool, c, nb *Chunk, dx, dz int32) {
	e.chunk(c.X<<4, c.Z<<4) // take the locks before reading
	e.chunk(nb.X<<4, nb.Z<<4)
	arr, other := c.BlockLight, nb.BlockLight
	if sky {
		arr, other = c.SkyLight, nb.SkyLight
	}
	bx, bz := c.X<<4, c.Z<<4
	for si := range c.Sections {
		a, o := arr[si+1], other[si+1]
		if (a.IsFull() && o.IsFull()) || a.IsZero() {
			continue
		}
		y0 := int32(c.Dim.MinY + si<<4)
		for i := int32(0); i < 16; i++ {
			x, z, ox, oz := i, i, i, i
			switch {
			case dx < 0:
				x, ox = 0, 15
			case dx > 0:
				x, ox = 15, 0
			case dz < 0:
				z, oz = 0, 15
			default:
				z, oz = 15, 0
			}
			col, ocol := int(z)<<4|int(x), int(oz)<<4|int(ox)
			for ly := range 16 {
				l := a.Get(ly<<8 | col)
				if l > 1 && l > o.Get(ly<<8|ocol)+1 {
					e.inc.push(bx+x, y0+int32(ly), bz+z, l)
				}
			}
		}
	}
}

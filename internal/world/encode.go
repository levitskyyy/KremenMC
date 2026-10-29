package world

import (
	"github.com/levitskyyy/KremenMC/internal/nbt"
	"github.com/levitskyyy/KremenMC/internal/proto"
)

// EncodeChunkData appends the body of Chunk Data and Update Light following
// the Chunk X/Z ints: heightmaps, section data, block entities and light.
// It takes the chunk's read lock.
func EncodeChunkData(w *proto.Writer, c *Chunk) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	encodeHeightmaps(w, c)
	size := 0
	for i := range c.Sections {
		s := &c.Sections[i]
		size += 4 + s.Blocks.NetSize() + s.Biomes.NetSize()
	}
	w.VarInt(int32(size))
	for i := range c.Sections {
		s := &c.Sections[i]
		w.I16(int16(s.nonAir))
		w.I16(int16(s.fluids))
		s.Blocks.Encode(w)
		s.Biomes.Encode(w)
	}
	encodeBlockEntities(w, c)
	encodeLight(w, c)
}

// EncodeLight appends the Light Data structure used by Update Light
// (after the chunk X/Z VarInts). It takes the chunk's read lock.
func EncodeLight(w *proto.Writer, c *Chunk) {
	c.mu.RLock()
	encodeLight(w, c)
	c.mu.RUnlock()
}

func encodeHeightmaps(w *proto.Writer, c *Chunk) {
	b := HeightmapBits(c.Dim.Height)
	pl := 64 / int(b)
	n := longsFor(256, b)
	w.VarInt(int32(len(clientHeightmaps)))
	for _, slot := range clientHeightmaps {
		w.VarInt(int32(heightmapKinds[slot]))
		w.VarInt(int32(n))
		h := &c.heights[slot]
		for l := 0; l < n; l++ {
			var v uint64
			for j := 0; j < pl; j++ {
				i := l*pl + j
				if i >= 256 {
					break
				}
				v |= uint64(h[i]) << (uint(j) * uint(b))
			}
			w.U64(v)
		}
	}
}

func encodeBlockEntities(w *proto.Writer, c *Chunk) {
	n := 0
	for i := range c.BlockEntities {
		if c.BlockEntities[i].NetType >= 0 {
			n++
		}
	}
	w.VarInt(int32(n))
	for i := range c.BlockEntities {
		be := &c.BlockEntities[i]
		if be.NetType < 0 {
			continue
		}
		w.U8(be.X<<4 | be.Z&15)
		w.I16(int16(be.Y))
		w.VarInt(be.NetType)
		b, err := nbt.Marshal(w.B, "", be.Data, true)
		if err != nil {
			// keep the stream aligned: an empty compound
			b = append(w.B, nbt.TagCompound, nbt.TagEnd)
		}
		w.B = b
	}
}

type bitset [4]uint64 // up to 256 light sections (4064 blocks + 2)

func (b *bitset) set(i int) { b[i>>6] |= 1 << uint(i&63) }

// write uses ByteBufCodecs.BIT_SET: a length-prefixed BitSet.toByteArray
// (little-endian, trailing zero bytes trimmed), not the long array of
// FriendlyByteBuf.writeBitSet.
func (b *bitset) write(w *proto.Writer) {
	n := len(b) * 8
	for n > 0 && byte(b[(n-1)>>3]>>(uint((n-1)&7)*8)) == 0 {
		n--
	}
	w.VarInt(int32(n))
	for i := 0; i < n; i++ {
		w.U8(byte(b[i>>3] >> (uint(i&7) * 8)))
	}
}

func encodeLight(w *proto.Writer, c *Chunk) {
	var sky, blk, emptySky, emptyBlk bitset
	nSky, nBlk := 0, 0
	if c.LightReady {
		for i := range c.BlockLight {
			if c.Dim.HasSkylight {
				if c.SkyLight[i].IsZero() {
					emptySky.set(i)
				} else {
					sky.set(i)
					nSky++
				}
			}
			if c.BlockLight[i].IsZero() {
				emptyBlk.set(i)
			} else {
				blk.set(i)
				nBlk++
			}
		}
	}
	sky.write(w)
	blk.write(w)
	emptySky.write(w)
	emptyBlk.write(w)
	w.VarInt(int32(nSky))
	if nSky > 0 {
		for i := range c.SkyLight {
			if sky[i>>6]&(1<<uint(i&63)) != 0 {
				w.VarInt(2048)
				w.Raw(c.SkyLight[i].Bytes())
			}
		}
	}
	w.VarInt(int32(nBlk))
	if nBlk > 0 {
		for i := range c.BlockLight {
			if blk[i>>6]&(1<<uint(i&63)) != 0 {
				w.VarInt(2048)
				w.Raw(c.BlockLight[i].Bytes())
			}
		}
	}
}

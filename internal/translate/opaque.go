package translate

import (
	"errors"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/translate/pdef"
)

var (
	errUnknownValue = errors.New("translate: value has no counterpart")
	errCount        = errors.New("translate: bad count")
)

// blockParticles carry a block state as their only payload.
var blockParticles = map[string]bool{
	"block": true, "block_marker": true, "falling_dust": true, "dust_pillar": true, "block_crumble": true,
}

// particle rewrites one canonical Particle. If the target lacks it the value
// is consumed and errUnknownValue returned with nothing written.
func (rw *itemRewriter) particle(r *proto.Reader, w *proto.Writer) error {
	m := rw.m
	t, err := r.VarInt()
	if err != nil {
		return err
	}
	if t < 0 || int(t) >= len(m.srcPart) || m.srcPart[t] == nil {
		return errUnknownValue
	}
	node := m.srcPart[t]
	tt := m.particles[t]
	if tt < 0 {
		if err := pdef.Skip(node, r); err != nil {
			return err
		}
		return errUnknownValue
	}
	if w != nil {
		w.VarInt(tt)
	}
	if blockParticles[m.partNames[t]] {
		s, err := r.VarInt()
		if err != nil {
			return err
		}
		if w != nil {
			w.VarInt(m.state(s))
		}
		return nil
	}
	buf, wk, err := rw.enter()
	if err != nil {
		return err
	}
	defer rw.leave()
	if err := wk.Copy(node, r, buf); err != nil {
		return err
	}
	if w != nil {
		w.Raw(buf.B)
	}
	return nil
}

// particleHook is used where a particle must be present: unknown particles
// become the target's fallback particle.
func (rw *itemRewriter) particleHook(r *proto.Reader, w *proto.Writer) error {
	mark := 0
	if w != nil {
		mark = len(w.B)
	}
	err := rw.particle(r, w)
	if errors.Is(err, errUnknownValue) {
		if w != nil {
			w.B = w.B[:mark]
			w.VarInt(rw.m.fallbackParticle)
		}
		return nil
	}
	return err
}

// metadata rewrites an entity metadata list for an entity of canonical type
// typ (-1 if unknown, keeping indices). Entries the target cannot represent
// are dropped.
func (rw *itemRewriter) metadata(r *proto.Reader, w *proto.Writer, typ int32) error {
	m := rw.m
	var idx []int16
	if typ >= 0 && int(typ) < len(m.metaIndex) {
		idx = m.metaIndex[typ]
	}
	for {
		key, err := r.U8()
		if err != nil {
			return err
		}
		if key == 0xff {
			w.U8(0xff)
			return nil
		}
		ser, err := r.VarInt()
		if err != nil {
			return err
		}
		if ser < 0 || int(ser) >= len(m.srcSer) || m.srcSer[ser] == nil {
			return errUnknownValue
		}
		node := m.srcSer[ser]
		tKey := int(key)
		if idx != nil {
			if int(key) >= len(idx) {
				tKey = -1
			} else {
				tKey = int(idx[key])
			}
		}
		tSer := m.serializers[ser]
		if tKey < 0 || tSer < 0 {
			if err := pdef.Skip(node, r); err != nil {
				return err
			}
			continue
		}
		mark := len(w.B)
		w.U8(uint8(tKey))
		w.VarInt(tSer)
		switch m.serNames[ser] {
		case "block_state":
			s, err := r.VarInt()
			if err != nil {
				return err
			}
			w.VarInt(m.state(s))
		case "optional_block_state":
			s, err := r.VarInt()
			if err != nil {
				return err
			}
			if s != 0 {
				s = m.state(s-1) + 1
			}
			w.VarInt(s)
		case "particle":
			if err := rw.particle(r, w); err != nil {
				if !errors.Is(err, errUnknownValue) {
					return err
				}
				w.B = w.B[:mark]
			}
		case "particles":
			if err := rw.particleList(r, w); err != nil {
				return err
			}
		default:
			if reg := variantRegistry[m.serNames[ser]]; reg != "" {
				v, err := r.VarInt()
				if err != nil {
					return err
				}
				tv := rw.c.regIndex(reg, v)
				if tv < 0 {
					w.B = w.B[:mark]
					continue
				}
				w.VarInt(tv)
				continue
			}
			buf, wk, err := rw.enter()
			if err != nil {
				return err
			}
			err = wk.Copy(node, r, buf)
			if err == nil {
				w.Raw(buf.B)
			}
			rw.leave()
			if err != nil {
				return err
			}
		}
	}
}

func (rw *itemRewriter) particleList(r *proto.Reader, w *proto.Writer) error {
	n, err := r.VarInt()
	if err != nil {
		return err
	}
	if n < 0 || int(n) > r.Remaining() {
		return errCount
	}
	buf, _, err := rw.enter()
	if err != nil {
		return err
	}
	defer rw.leave()
	var kept int32
	for range n {
		mark := len(buf.B)
		if err := rw.particle(r, buf); err != nil {
			if !errors.Is(err, errUnknownValue) {
				return err
			}
			buf.B = buf.B[:mark]
			continue
		}
		kept++
	}
	w.VarInt(kept)
	w.Raw(buf.B)
	return nil
}

// variantRegistry names the synced registry behind metadata serializers that
// hold a plain registry index.
var variantRegistry = map[string]string{
	"cat_variant":             "cat_variant",
	"cat_sound_variant":       "cat_sound_variant",
	"cow_variant":             "cow_variant",
	"cow_sound_variant":       "cow_sound_variant",
	"wolf_variant":            "wolf_variant",
	"wolf_sound_variant":      "wolf_sound_variant",
	"frog_variant":            "frog_variant",
	"pig_variant":             "pig_variant",
	"pig_sound_variant":       "pig_sound_variant",
	"chicken_variant":         "chicken_variant",
	"chicken_sound_variant":   "chicken_sound_variant",
	"zombie_nautilus_variant": "zombie_nautilus_variant",
}

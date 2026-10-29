package translate

import (
	"errors"
	"strings"

	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
)

// scratch holds per-connection copies of canonical packets whose values are
// rewritten before encoding, so shared packets are never mutated.
type scratch struct {
	blockUpdate   packets.ClientboundBlockUpdate
	sectionUpdate packets.ClientboundSectionBlocksUpdate
	chunk         packets.ClientboundLevelChunkWithLight
	chunkData     []byte
	chunkVals     []uint32
	biomes        packets.ClientboundChunksBiomes
	biomeData     []byte
	addEntity     packets.ClientboundAddEntity
	blockEvent    packets.ClientboundBlockEvent
	levelEvent    packets.ClientboundLevelEvent
	transient     packets.ClientboundAddTransientBlock
	entityData    packets.ClientboundSetEntityData
	setContent    packets.ClientboundContainerSetContent
	setSlot       packets.ClientboundContainerSetSlot
	cursor        packets.ClientboundSetCursorItem
	inventory     packets.ClientboundSetPlayerInventory
	equipment     packets.ClientboundSetEquipment
	particles     packets.ClientboundLevelParticles
	explode       packets.ClientboundExplode
	sound         packets.ClientboundSound
	soundEntity   packets.ClientboundSoundEntity
	soundValue    packets.ItemSoundEvent
	attributes    packets.ClientboundUpdateAttributes
	login         packets.ClientboundLogin
	respawn       packets.ClientboundRespawn
	damage        packets.ClientboundDamageEvent
	playerChat    packets.ClientboundPlayerChat
	disguisedChat packets.ClientboundDisguisedChat
	commands      packets.ClientboundCommands
	raw           proto.Writer
}

// syncedReg maps the indices of one data-driven registry as sent by the
// server to the indices the client assigned after filtering.
type syncedReg struct {
	toTarget []int32
	names    []string // canonical entry keys without namespace
	n        int
}

// regName returns the canonical key of a synced registry entry.
func (c *conn) regName(reg string, v int32) (string, bool) {
	r := c.regs[reg]
	if r == nil || v < 0 || int(v) >= len(r.names) {
		return "", false
	}
	return r.names[v], true
}

// regIndex translates a synced registry index; unobserved registries pass
// through unchanged.
func (c *conn) regIndex(reg string, v int32) int32 {
	r := c.regs[reg]
	if r == nil {
		return v
	}
	return at(r.toTarget, v)
}

func trimNS(s string) string {
	s, _ = strings.CutPrefix(s, "minecraft:")
	return s
}

func (c *conn) rewriteCB(s packets.State, p packets.Packet) (packets.Packet, error) {
	m, sc := c.t.maps, &c.scratch
	switch q := p.(type) {
	case *packets.ClientboundConfigSelectKnownPacks:
		return c.knownPacks(q), nil
	case *packets.ClientboundConfigRegistryData:
		return c.registryData(q), nil
	case *packets.ClientboundConfigUpdateTags:
		return c.configTags(q), nil
	case *packets.ClientboundUpdateTags:
		return c.playTags(q), nil

	case *packets.ClientboundBlockUpdate:
		sc.blockUpdate = *q
		sc.blockUpdate.Type = m.state(q.Type)
		return &sc.blockUpdate, nil
	case *packets.ClientboundAddTransientBlock:
		sc.transient = *q
		sc.transient.BlockState = m.state(q.BlockState)
		return &sc.transient, nil
	case *packets.ClientboundSectionBlocksUpdate:
		sc.sectionUpdate.ChunkCoordinates = q.ChunkCoordinates
		sc.sectionUpdate.Records = append(sc.sectionUpdate.Records[:0], q.Records...)
		for i, r := range sc.sectionUpdate.Records {
			sc.sectionUpdate.Records[i] = int64(m.state(int32(r>>12)))<<12 | r&0xfff
		}
		return &sc.sectionUpdate, nil
	case *packets.ClientboundLevelChunkWithLight:
		var err error
		sc.chunk = *q
		sc.chunkData, sc.chunkVals, err = rewriteSections(sc.chunkData[:0], q.ChunkData, c.t.last().chunk, &c.t.blocks, &c.biomes, sc.chunkVals)
		if err != nil {
			return nil, err
		}
		sc.chunk.ChunkData = sc.chunkData
		return &sc.chunk, nil
	case *packets.ClientboundChunksBiomes:
		sc.biomes.Biomes = append(sc.biomes.Biomes[:0], q.Biomes...)
		sc.biomeData = sc.biomeData[:0]
		for i := range sc.biomes.Biomes {
			var err error
			start := len(sc.biomeData)
			sc.biomeData, sc.chunkVals, err = rewriteBiomes(sc.biomeData, q.Biomes[i].Data, c.t.last().chunk, &c.biomes, sc.chunkVals)
			if err != nil {
				return nil, err
			}
			sc.biomes.Biomes[i].Data = sc.biomeData[start:len(sc.biomeData):len(sc.biomeData)]
		}
		return &sc.biomes, nil
	case *packets.ClientboundBlockEvent:
		b := m.block(q.BlockID)
		if b < 0 {
			return nil, nil
		}
		sc.blockEvent = *q
		sc.blockEvent.BlockID = b
		return &sc.blockEvent, nil
	case *packets.ClientboundLevelEvent:
		if q.EffectID != 2001 && q.EffectID != 3008 {
			return p, nil
		}
		sc.levelEvent = *q
		sc.levelEvent.Data = m.state(q.Data)
		return &sc.levelEvent, nil

	case *packets.ClientboundLogin:
		c.loaded.Store(false)
		c.entities = map[int32]int32{}
		if pl, ok := c.t.vers[0].reg.EntityByName("player"); ok {
			c.entities[q.EntityID] = int32(pl.ID)
		}
		sc.login = *q
		sc.login.WorldState.Dimension = c.regIndex("dimension_type", q.WorldState.Dimension)
		return &sc.login, nil
	case *packets.ClientboundPlayerPosition:
		if q.Flags&0x1f == 0 {
			c.recordTeleport(q)
		}
		return p, nil
	case *packets.ClientboundRespawn:
		c.loaded.Store(false)
		sc.respawn = *q
		sc.respawn.WorldState.Dimension = c.regIndex("dimension_type", q.WorldState.Dimension)
		return &sc.respawn, nil
	case *packets.ClientboundPlayerChat:
		if q.Type.Value != nil {
			return p, nil
		}
		t := c.regIndex("chat_type", q.Type.ID)
		if t < 0 {
			return nil, nil
		}
		sc.playerChat = *q
		sc.playerChat.Type.ID = t
		return &sc.playerChat, nil
	case *packets.ClientboundDisguisedChat:
		if q.Type.Value != nil {
			return p, nil
		}
		t := c.regIndex("chat_type", q.Type.ID)
		if t < 0 {
			return nil, nil
		}
		sc.disguisedChat = *q
		sc.disguisedChat.Type.ID = t
		return &sc.disguisedChat, nil
	case *packets.ClientboundDamageEvent:
		t := c.regIndex("damage_type", q.SourceTypeID)
		if t < 0 {
			return nil, nil
		}
		sc.damage = *q
		sc.damage.SourceTypeID = t
		return &sc.damage, nil
	case *packets.ClientboundAddEntity:
		t := m.entity(q.Type)
		if t < 0 {
			return nil, nil
		}
		c.entities[q.EntityID] = q.Type
		sc.addEntity = *q
		sc.addEntity.Type = t
		return &sc.addEntity, nil
	case *packets.ClientboundRemoveEntities:
		for _, id := range q.EntityIDs {
			delete(c.entities, id)
		}
		return p, nil
	case *packets.ClientboundSetEntityData:
		typ, ok := c.entities[q.EntityID]
		if !ok {
			typ = -1
		}
		w := c.rawW()
		if err := c.cbItems.metadata(&proto.Reader{B: q.Raw}, w, typ); err != nil {
			return dropOn(err)
		}
		sc.entityData = packets.ClientboundSetEntityData{EntityID: q.EntityID, Raw: w.B}
		return &sc.entityData, nil

	case *packets.ClientboundContainerSetContent:
		w := c.rawW()
		r := proto.Reader{B: q.Raw}
		n, err := r.VarInt()
		if err != nil || n < 0 || int(n) > r.Remaining() {
			return nil, nil
		}
		w.VarInt(n)
		for range n + 1 {
			if err := c.cbItems.slot(&r, w); err != nil {
				return dropOn(err)
			}
		}
		sc.setContent = packets.ClientboundContainerSetContent{WindowID: q.WindowID, StateID: q.StateID, Raw: w.B}
		return &sc.setContent, nil
	case *packets.ClientboundContainerSetSlot:
		w, err := c.oneSlot(q.Raw)
		if err != nil {
			return dropOn(err)
		}
		sc.setSlot = packets.ClientboundContainerSetSlot{WindowID: q.WindowID, StateID: q.StateID, Slot: q.Slot, Raw: w.B}
		return &sc.setSlot, nil
	case *packets.ClientboundSetCursorItem:
		w, err := c.oneSlot(q.Raw)
		if err != nil {
			return dropOn(err)
		}
		sc.cursor = packets.ClientboundSetCursorItem{Raw: w.B}
		return &sc.cursor, nil
	case *packets.ClientboundSetPlayerInventory:
		w, err := c.oneSlot(q.Raw)
		if err != nil {
			return dropOn(err)
		}
		sc.inventory = packets.ClientboundSetPlayerInventory{SlotID: q.SlotID, Raw: w.B}
		return &sc.inventory, nil
	case *packets.ClientboundSetEquipment:
		w := c.rawW()
		r := proto.Reader{B: q.Raw}
		for {
			b, err := r.U8()
			if err != nil {
				return nil, nil
			}
			w.U8(b)
			if err := c.cbItems.slot(&r, w); err != nil {
				return dropOn(err)
			}
			if b&0x80 == 0 {
				break
			}
		}
		if r.Remaining() != 0 {
			return nil, nil
		}
		sc.equipment = packets.ClientboundSetEquipment{EntityID: q.EntityID, Raw: w.B}
		return &sc.equipment, nil
	case *packets.ClientboundLevelParticles:
		w := c.rawW()
		r := proto.Reader{B: q.Raw}
		if err := c.cbItems.particle(&r, w); err != nil {
			return dropOn(err)
		}
		w.Raw(r.B[r.Off:])
		sc.particles = packets.ClientboundLevelParticles{Raw: w.B}
		return &sc.particles, nil
	case *packets.ClientboundExplode:
		return c.explode(q)

	case *packets.ClientboundSound:
		h, ok := c.soundHolder(q.Sound)
		if !ok {
			return nil, nil
		}
		sc.sound = *q
		sc.sound.Sound = h
		sc.sound.SoundCategory = c.soundSource(q.SoundCategory)
		return &sc.sound, nil
	case *packets.ClientboundSoundEntity:
		h, ok := c.soundHolder(q.Sound)
		if !ok {
			return nil, nil
		}
		sc.soundEntity = *q
		sc.soundEntity.Sound = h
		sc.soundEntity.SoundCategory = c.soundSource(q.SoundCategory)
		return &sc.soundEntity, nil
	case *packets.ClientboundUpdateAttributes:
		sc.attributes.EntityID = q.EntityID
		sc.attributes.Properties = sc.attributes.Properties[:0]
		for _, pr := range q.Properties {
			if k := at(m.attributes, pr.Key); k >= 0 {
				pr.Key = k
				sc.attributes.Properties = append(sc.attributes.Properties, pr)
			}
		}
		return &sc.attributes, nil
	case *packets.ClientboundCommands:
		w := c.rawW()
		if err := c.commands(q.Raw, w); err != nil {
			return dropOn(err)
		}
		sc.commands = packets.ClientboundCommands{Raw: w.B}
		return &sc.commands, nil

	case *packets.ClientboundUpdateRecipes, *packets.ClientboundRecipeBookAdd, *packets.ClientboundPlaceGhostRecipe,
		*packets.ClientboundUpdateAdvancements, *packets.ClientboundMerchantOffers:
		// item displays and costs are not rewritten yet
		return nil, nil
	}
	return p, nil
}

// dropOn turns value errors into a dropped packet; malformed canonical data
// is still reported.
func dropOn(err error) (packets.Packet, error) {
	if errors.Is(err, errUnknownValue) || errors.Is(err, errDepth) {
		return nil, nil
	}
	return nil, err
}

func (c *conn) rawW() *proto.Writer {
	w := &c.scratch.raw
	w.B = w.B[:0]
	return w
}

func (c *conn) oneSlot(raw []byte) (*proto.Writer, error) {
	w := c.rawW()
	r := proto.Reader{B: raw}
	if err := c.cbItems.slot(&r, w); err != nil {
		return nil, err
	}
	w.Raw(r.B[r.Off:])
	return w, nil
}

func (c *conn) soundHolder(h proto.Holder[packets.ItemSoundEvent]) (proto.Holder[packets.ItemSoundEvent], bool) {
	if h.Value != nil {
		return h, true
	}
	if t := at(c.t.maps.sounds, h.ID); t >= 0 {
		return proto.Holder[packets.ItemSoundEvent]{ID: t}, true
	}
	names := c.t.vers[0].meta.Enums["sound"]
	if h.ID < 0 || int(h.ID) >= len(names) {
		return h, false
	}
	c.scratch.soundValue = packets.ItemSoundEvent{SoundName: "minecraft:" + names[h.ID]}
	return proto.Holder[packets.ItemSoundEvent]{Value: &c.scratch.soundValue}, true
}

func (c *conn) soundSource(v int32) int32 {
	if t := at(c.t.maps.soundSources, v); t >= 0 {
		return t
	}
	return 0
}

func (c *conn) explode(q *packets.ClientboundExplode) (packets.Packet, error) {
	w := c.rawW()
	r := proto.Reader{B: q.Raw}
	rw := c.cbItems
	if err := rw.particleHook(&r, w); err != nil {
		return dropOn(err)
	}
	var h proto.Holder[packets.ItemSoundEvent]
	if err := decodeSoundHolder(&r, &h); err != nil {
		return nil, err
	}
	h, ok := c.soundHolder(h)
	if !ok {
		return nil, nil
	}
	encodeSoundHolder(w, h)
	n, err := r.VarInt()
	if err != nil || n < 0 || int(n) > r.Remaining() {
		return nil, nil
	}
	w.VarInt(n)
	for range n {
		if err := rw.particleHook(&r, w); err != nil {
			return dropOn(err)
		}
		rest, err := r.Raw(8) // scaling f32, speed f32
		if err != nil {
			return nil, err
		}
		w.Raw(rest)
		weight, err := r.VarInt()
		if err != nil {
			return nil, err
		}
		w.VarInt(weight)
	}
	w.Raw(r.B[r.Off:])
	sc := &c.scratch
	sc.explode = *q
	sc.explode.Raw = w.B
	return &sc.explode, nil
}

func decodeSoundHolder(r *proto.Reader, h *proto.Holder[packets.ItemSoundEvent]) error {
	id, err := r.VarInt()
	if err != nil {
		return err
	}
	if id != 0 {
		*h = proto.Holder[packets.ItemSoundEvent]{ID: id - 1}
		return nil
	}
	v := new(packets.ItemSoundEvent)
	if err := v.Decode(r); err != nil {
		return err
	}
	*h = proto.Holder[packets.ItemSoundEvent]{Value: v}
	return nil
}

func encodeSoundHolder(w *proto.Writer, h proto.Holder[packets.ItemSoundEvent]) {
	if h.Value != nil {
		w.VarInt(0)
		h.Value.Encode(w)
		return
	}
	w.VarInt(h.ID + 1)
}

func (c *conn) rewriteSB(s packets.State, p packets.Packet) (packets.Packet, error) {
	switch q := p.(type) {
	case *packets.ServerboundMovePlayerRot:
		c.yaw, c.pitch = q.Yaw, q.Pitch
	case *packets.ServerboundMovePlayerPosRot:
		c.yaw, c.pitch = q.Yaw, q.Pitch
	case *packets.ServerboundConfigSelectKnownPacks:
		out := &packets.ServerboundConfigSelectKnownPacks{}
		for _, pk := range q.Packs {
			if pk.Namespace == "minecraft" && pk.ID == "core" {
				if c.isTargetPack(pk.Version) {
					pk.Version = packets.Version
				}
			}
			out.Packs = append(out.Packs, pk)
		}
		return out, nil
	case *packets.ServerboundSetCreativeModeSlot:
		w := &c.sbW
		w.B = w.B[:0]
		r := proto.Reader{B: q.Raw}
		if err := c.sbItems.untrusted(&r, w); err != nil {
			return nil, err
		}
		w.Raw(r.B[r.Off:])
		return &packets.ServerboundSetCreativeModeSlot{Slot: q.Slot, Raw: append([]byte(nil), w.B...)}, nil
	case *packets.ServerboundContainerClick:
		w := &c.sbW
		w.B = w.B[:0]
		r := proto.Reader{B: q.Raw}
		if err := c.containerClick(&r, w); err != nil {
			return nil, err
		}
		cp := *q
		cp.Raw = append([]byte(nil), w.B...)
		return &cp, nil
	}
	return p, nil
}

func (c *conn) containerClick(r *proto.Reader, w *proto.Writer) error {
	n, err := r.VarInt()
	if err != nil {
		return err
	}
	if n < 0 || int(n) > r.Remaining() {
		return errCount
	}
	w.VarInt(n)
	for range n {
		slot, err := r.I16()
		if err != nil {
			return err
		}
		w.I16(slot)
		if err := c.optHashed(r, w); err != nil {
			return err
		}
	}
	if err := c.optHashed(r, w); err != nil {
		return err
	}
	w.Raw(r.B[r.Off:])
	return nil
}

func (c *conn) optHashed(r *proto.Reader, w *proto.Writer) error {
	ok, err := r.Bool()
	if err != nil {
		return err
	}
	w.Bool(ok)
	if !ok {
		return nil
	}
	return c.sbItems.hashed(r, w)
}

func (c *conn) isTargetPack(v string) bool {
	for _, p := range c.t.last().meta.Packs {
		if p == v {
			return true
		}
	}
	return false
}

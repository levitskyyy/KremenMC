package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/internal/data"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/proto"
	"github.com/levitskyyy/KremenMC/internal/proto/packets"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// Entity and block sounds. Per-type names, categories, volumes and ambient
// intervals are dumped from the reference by tools/refprobe
// kremenmc.probe.sound.MobTable and SoundTypes; pitch, range and trigger
// rules were observed with kremenmc.probe.sound.Sounds and Ambient.
//
//go:embed refdata/block_sounds.txt.gz
var blockSoundsGz []byte

//go:embed refdata/mob_sounds.txt.gz
var mobSoundsGz []byte

// Sound categories (SoundSource ordinals) not declared elsewhere.
const (
	soundAmbient = 8
)

var sndIDs sync.Map // name -> int32

// snd is soundID with a cache; it accepts names with or without namespace.
func snd(name string) int32 {
	if v, ok := sndIDs.Load(name); ok {
		return v.(int32)
	}
	id := soundID(ns(name))
	sndIDs.Store(name, id)
	return id
}

// soundAt plays sound id at pos for every player in range except one
// (Level.playSound with a player argument). Range is 16 blocks, scaled by
// volume above 1.
func (w *World) soundAt(pos api.Vec3, id, cat int32, vol, pitch float32, except *Player) {
	if id < 0 || w == nil {
		return
	}
	r := 16 * max(float64(vol), 1)
	var raw []byte
	for _, o := range w.playerList() {
		if o == except || o.Location().Pos.DistanceSq(pos) > r*r {
			continue
		}
		if raw == nil {
			raw = encode(&packets.ClientboundSound{
				Sound: proto.Holder[packets.ItemSoundEvent]{ID: id}, SoundCategory: cat,
				X: int32(pos.X * 8), Y: int32(pos.Y * 8), Z: int32(pos.Z * 8),
				Volume: vol, Pitch: pitch, Seed: rand.Int64(),
			})
		}
		o.c.SendRaw(raw)
	}
}

// --- block sound types ---

type blockSound struct {
	vol, pitch                  float32
	brk, step, place, hit, fall int32
}

type blockSoundTab struct {
	types []blockSound
	state []uint8 // by StateID
}

var blockSounds = sync.OnceValue(func() *blockSoundTab {
	t := &blockSoundTab{state: make([]uint8, data.NumStates())}
	reg := data.Canonical()
	zr, err := gzip.NewReader(bytes.NewReader(blockSoundsGz))
	if err != nil {
		return t
	}
	sc := bufio.NewScanner(zr)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) == 9 && f[0] == "t":
			v, _ := strconv.ParseFloat(f[2], 32)
			p, _ := strconv.ParseFloat(f[3], 32)
			t.types = append(t.types, blockSound{vol: float32(v), pitch: float32(p),
				brk: snd(f[4]), step: snd(f[5]), place: snd(f[6]), hit: snd(f[7]), fall: snd(f[8])})
		case len(f) == 3 && f[0] == "b":
			b, ok := reg.BlockByName(f[1])
			if !ok {
				continue
			}
			idx := strings.Split(f[2], ",")
			for i := 0; i < int(b.NumStates); i++ {
				n, _ := strconv.Atoi(idx[min(i, len(idx)-1)])
				t.state[int(b.MinState)+i] = uint8(n)
			}
		}
	}
	return t
})

// blockSoundOf is BlockState.getSoundType.
func blockSoundOf(s data.StateID) *blockSound {
	t := blockSounds()
	if len(t.types) == 0 {
		return &blockSound{vol: 1, pitch: 1, brk: -1, step: -1, place: -1, hit: -1, fall: -1}
	}
	if int(s) >= len(t.state) {
		return &t.types[0]
	}
	return &t.types[t.state[s]]
}

// placeSound is BlockItem.place's sound: heard by everyone but the placer,
// volume (v+1)/2, pitch p*0.8.
func (w *World) placeSound(pos api.BlockPos, st data.StateID, placer *Player) {
	t := blockSoundOf(st)
	w.soundAt(pos.Center(), t.place, soundBlocks, (t.vol+1)/2, t.pitch*0.8, placer)
}

// --- mob sound table ---

type mobSound struct {
	cat                  int32
	vol                  float32
	interval             int32
	ambient, hurt, death string
}

var mobSounds = sync.OnceValue(func() map[string][2]*mobSound {
	out := map[string][2]*mobSound{}
	zr, err := gzip.NewReader(bytes.NewReader(mobSoundsGz))
	if err != nil {
		return out
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 8 {
			continue
		}
		v, _ := strconv.ParseFloat(f[3], 32)
		iv, _ := strconv.Atoi(f[4])
		ms := &mobSound{cat: int32(max(indexOf(soundCategories, f[2]), 0)), vol: float32(v), interval: int32(iv), ambient: f[5], hurt: f[6], death: f[7]}
		e := out[f[0]]
		if f[1] == "baby" {
			e[1] = ms
		} else {
			e[0] = ms
		}
		out[f[0]] = e
	}
	return out
})

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// soundVariant returns the named sound ("hurt_sound", ...) of a mob's
// data-driven sound variant (cow, pig, chicken, cat, wolf), or "".
func soundVariant(m *Mob, baby bool, field string) string {
	reg := m.typ.Name + "_sound_variant"
	if _, ok := holderMeta[m.typ.Name]; !ok {
		return ""
	}
	m.life.mu.Lock()
	v := m.life.vals["sound_variant"]
	m.life.mu.Unlock()
	var key string
	switch v := v.(type) {
	case string:
		key = ns(v)
	case int32:
		for _, r := range regsync.Registries() {
			if r.Name == "minecraft:"+reg && int(v) < len(r.Entries) && v >= 0 {
				key = r.Entries[v]
			}
		}
	}
	if key == "" {
		key = "minecraft:classic"
	}
	vs := variantSounds(reg, key)
	if vs == nil {
		return ""
	}
	if baby {
		return vs[1][field]
	}
	return vs[0][field]
}

var variantCache sync.Map // reg#key -> *[2]map[string]string

func variantSounds(reg, key string) *[2]map[string]string {
	k := reg + "#" + key
	if v, ok := variantCache.Load(k); ok {
		return v.(*[2]map[string]string)
	}
	var out *[2]map[string]string
	if p, err := wgdata.Load(); err == nil {
		if raw, ok := p.JSON(reg, key); ok {
			var j struct {
				Adult map[string]string `json:"adult_sounds"`
				Baby  map[string]string `json:"baby_sounds"`
			}
			var flat map[string]any
			if json.Unmarshal(raw, &j) == nil && j.Adult != nil {
				out = &[2]map[string]string{j.Adult, j.Baby}
			} else if json.Unmarshal(raw, &flat) == nil {
				m := map[string]string{}
				for k, v := range flat {
					if s, ok := v.(string); ok {
						m[k] = s
					}
				}
				out = &[2]map[string]string{m, m}
			}
		}
	}
	variantCache.Store(k, out)
	return out
}

func (m *Mob) sounds() (*mobSound, bool) {
	e := mobSounds()[m.typ.Name]
	baby := m.IsBaby()
	if baby && e[1] != nil {
		return e[1], true
	}
	return e[0], baby
}

// voicePitch is LivingEntity.getVoicePitch.
func voicePitch(baby bool) float32 {
	p := (rand.Float32()-rand.Float32())*0.2 + 1
	if baby {
		p += 0.5
	}
	return p
}

func (m *Mob) slimeSize() int32 {
	if b := m.brain(); b != nil {
		return max(b.slimeSize, 1)
	}
	return 1
}

// mobVolume is getSoundVolume (slimes scale with size).
func (m *Mob) mobVolume(ms *mobSound) float32 {
	switch m.typ.Name {
	case "slime", "magma_cube", "sulfur_cube":
		return 0.4 * float32(m.slimeSize())
	}
	return ms.vol
}

// sizedSound swaps the small-slime sound for the big one on large slimes.
func (m *Mob) sizedSound(name string) string {
	if m.slimeSize() > 1 {
		if s, ok := strings.CutSuffix(name, "_small"); ok {
			if strings.HasPrefix(s, "entity.small_sulfur_cube.") {
				s = "entity.sulfur_cube." + strings.TrimPrefix(s, "entity.small_sulfur_cube.")
			}
			if snd(s) >= 0 {
				return s
			}
		}
	}
	return name
}

// mobSoundName applies sound variants and in-water forms to a table name.
func (m *Mob) mobSoundName(base string, baby bool, field string) string {
	if v := soundVariant(m, baby, field); v != "" {
		base = v
	}
	if s, ok := strings.CutSuffix(base, "_land"); ok && m.inWater() && snd(s) >= 0 {
		base = s
	}
	return m.sizedSound(base)
}

func (m *Mob) inWater() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.e.InWater
}

// playHurtSound is LivingEntity.playHurtSound (or the death sound when the
// hit was fatal). Players pick the sound from the damage type's effects and
// do not receive their own (their client plays it from the damage event).
func playHurtSound(t combatant, dt *damageType, dead bool) {
	w := t.wld()
	switch v := t.(type) {
	case *Player:
		name := "entity.player.death"
		if !dead {
			name = playerHurtSound(dt)
		}
		w.soundAt(v.feet(), snd(name), soundPlayers, 1, 1, v)
	case *Mob:
		if v.Silent() {
			return
		}
		ms, baby := v.sounds()
		if ms == nil {
			return
		}
		name, field := ms.hurt, "hurt_sound"
		if dead {
			name, field = ms.death, "death_sound"
		}
		if name == "-" {
			return
		}
		name = v.mobSoundName(name, baby, field)
		w.soundAt(v.feet(), snd(name), ms.cat, v.mobVolume(ms), voicePitch(baby), nil)
		v.liv.mu.Lock()
		v.liv.snd.ambient = -ms.interval
		v.liv.mu.Unlock()
	}
}

// playerHurtSound maps DamageType.effects to the player's hurt sound.
func playerHurtSound(dt *damageType) string {
	switch dt.effects {
	case "thorns":
		return "enchant.thorns.hit"
	case "drowning":
		return "entity.player.hurt_drown"
	case "burning":
		return "entity.player.hurt_on_fire"
	case "poking":
		return "entity.player.hurt_sweet_berry_bush"
	case "freezing":
		return "entity.player.hurt_freeze"
	}
	return "entity.player.hurt"
}

// tickAmbient is Mob.baseTick's ambient sound roll: each tick the chance
// grows until rand(1000) < counter, which resets it to -interval.
func (m *Mob) tickAmbient() {
	ms, baby := m.sounds()
	if ms == nil || ms.ambient == "-" || ms.interval <= 0 {
		return
	}
	l := &m.liv
	l.mu.Lock()
	fire := rand.IntN(1000) < int(l.snd.ambient)
	l.snd.ambient++
	if fire {
		l.snd.ambient = -ms.interval
	}
	l.mu.Unlock()
	if !fire || m.Silent() {
		return
	}
	name, field := m.ambientName(ms.ambient, baby)
	name = m.mobSoundName(name, baby, field)
	m.w.soundAt(m.feet(), snd(name), ms.cat, m.mobVolume(ms), voicePitch(baby), nil)
}

// ambientName resolves state-dependent ambient sounds the table's fresh
// instance could not show.
func (m *Mob) ambientName(def string, baby bool) (string, string) {
	b := m.brain()
	switch m.typ.Name {
	case "wolf":
		switch {
		case b != nil && b.target != 0:
			return "entity.wolf.growl", "growl_sound"
		case rand.IntN(3) == 0:
			return "entity.wolf.pant", "pant_sound"
		}
		if baby {
			return "entity.baby_wolf.ambient", "ambient_sound"
		}
		return "entity.wolf.ambient", "ambient_sound"
	case "cat":
		m.life.mu.Lock()
		tame := m.life.tame
		m.life.mu.Unlock()
		if tame {
			if baby {
				return "entity.baby_cat.ambient", "ambient_sound"
			}
			return "entity.cat.ambient", "ambient_sound"
		}
		return def, "stray_ambient_sound"
	case "hoglin":
		return "entity.hoglin.ambient", ""
	case "piglin":
		return "entity.piglin.ambient", ""
	case "axolotl":
		if m.inWater() {
			return "entity.axolotl.idle_water", ""
		}
	}
	return def, "ambient_sound"
}

// --- steps and falls ---

type soundState struct {
	ambient  int32
	moveDist float64
	nextStep float64
}

// stepVolume overrides the 0.15 step volume of entity-specific steps.
var stepVolume = map[string]float32{"iron_golem": 1}

// stepSoundAlias names types that reuse another type's step sound.
var stepSoundAlias = map[string]string{"cave_spider": "spider", "mooshroom": "cow", "trader_llama": "llama", "zombified_piglin": "zombie"}

// stepped accumulates horizontal ground movement (Entity.move: moveDist
// += d*0.6) and plays a step sound each time it passes the next integer.
// except is the moving player, who predicts their own steps.
func stepped(t combatant, from, to api.Vec3, onGround bool, except *Player) {
	l := t.lv()
	dx, dz := to.X-from.X, to.Z-from.Z
	d := math.Sqrt(dx*dx + dz*dz)
	if !onGround || d == 0 || d > 8 {
		return
	}
	l.mu.Lock()
	l.snd.moveDist += d * 0.6
	play := l.snd.moveDist > l.snd.nextStep
	if play {
		l.snd.nextStep = math.Floor(l.snd.moveDist) + 1
	}
	l.mu.Unlock()
	if !play {
		return
	}
	w := t.wld()
	st, ok := stepBlock(w, to)
	if !ok {
		return
	}
	switch v := t.(type) {
	case *Player:
		if v.Sneaking() {
			return
		}
		bs := blockSoundOf(st)
		w.soundAt(to, bs.step, soundPlayers, bs.vol*0.15, bs.pitch, except)
	case *Mob:
		if v.Silent() {
			return
		}
		ms, baby := v.sounds()
		cat := int32(soundNeutral)
		if ms != nil {
			cat = ms.cat
		}
		if id := v.mobStepSound(baby); id >= 0 {
			vol, ok := stepVolume[v.typ.Name]
			if !ok {
				vol = 0.15
			}
			w.soundAt(to, id, cat, vol, 1, nil)
			return
		}
		bs := blockSoundOf(st)
		w.soundAt(to, bs.step, cat, bs.vol*0.15, bs.pitch, nil)
	}
}

func (m *Mob) mobStepSound(baby bool) int32 {
	if v := soundVariant(m, baby, "step_sound"); v != "" {
		return snd(v)
	}
	name := m.typ.Name
	if a, ok := stepSoundAlias[name]; ok {
		name = a
	}
	if baby {
		if id := snd("entity.baby_" + name + ".step"); id >= 0 {
			return id
		}
	}
	return snd("entity." + name + ".step")
}

// stepBlock is the block whose step sound plays: the one at the feet when
// it is a thin covering (snow layer, carpet), else the supporting block.
func stepBlock(w *World, pos api.Vec3) (data.StateID, bool) {
	x, z := floorInt(pos.X), floorInt(pos.Z)
	if at := w.w.Block(x, floorInt(pos.Y), z); !data.IsAir(at) {
		if b := data.BlockOf(at); b != nil && inTag("block", "minecraft:inside_step_sound_blocks", b.Name) {
			return at, true
		}
	}
	below := w.w.Block(x, floorInt(pos.Y-0.2), z)
	if data.IsAir(below) {
		below = w.w.Block(x, floorInt(pos.Y-1.2), z)
		if b := data.BlockOf(below); b == nil || !strings.HasSuffix(b.Name, "fence") && !strings.HasSuffix(b.Name, "wall") && !strings.HasSuffix(b.Name, "fence_gate") {
			return 0, false
		}
	}
	if data.IsAir(below) || data.Flags(below)&data.FlagFluid != 0 && data.Collision(below) == nil {
		return 0, false
	}
	return below, true
}

// fallSounds is the landing part of LivingEntity.causeFallDamage when it
// hurts: small (damage <= 4) or big fall, then the landed-on block's fall
// sound at half volume and 0.75 pitch.
func fallSounds(t combatant, damage float32) {
	w := t.wld()
	pos := t.feet()
	kind, cat := "entity.generic.", int32(soundNeutral)
	var self *Player
	switch v := t.(type) {
	case *Player:
		kind, cat, self = "entity.player.", soundPlayers, v
	case *Mob:
		if v.Silent() {
			return
		}
		if ms, _ := v.sounds(); ms != nil {
			cat = ms.cat
		}
		if v.typ.Category == "monster" {
			kind = "entity.hostile."
		}
	}
	size := "small_fall"
	if damage > 4 {
		size = "big_fall"
	}
	w.soundAt(pos, snd(kind+size), cat, 1, 1, self)
	below := w.w.Block(floorInt(pos.X), floorInt(pos.Y-0.2), floorInt(pos.Z))
	if !data.IsAir(below) {
		bs := blockSoundOf(below)
		w.soundAt(pos, bs.fall, cat, bs.vol*0.5, bs.pitch*0.75, self)
	}
}

// levelUpSound is Player.giveExperienceLevels' cue every fifth level.
func (p *Player) levelUpSound(level int32) {
	if level <= 0 || level%5 != 0 {
		return
	}
	now := p.s.CurrentTick()
	if p.use.lastLevelUp != 0 && now-p.use.lastLevelUp < 100 {
		return
	}
	p.use.lastLevelUp = now
	f := float32(1)
	if level <= 30 {
		f = float32(level) / 30
	}
	p.world.Load().soundAt(p.feet(), snd("entity.player.levelup"), soundPlayers, f*0.75, 1, nil)
}

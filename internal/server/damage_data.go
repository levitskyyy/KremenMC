package server

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/item"
	"github.com/levitskyyy/KremenMC/internal/net/regsync"
	"github.com/levitskyyy/KremenMC/internal/world/worldgen/wgdata"
)

// damageType is a minecraft:damage_type registry entry.
type damageType struct {
	key        string
	id         int32
	message    string
	deathMsg   string // death_message_type
	scaling    string // never, always, when_caused_by_living_non_player
	exhaustion float32
	effects    string // hurt, thorns, drowning, burning, poking, freezing
	tags       map[string]bool
}

func (d *damageType) is(tag string) bool { return d != nil && d.tags[tag] }

var (
	dmgOnce  sync.Once
	dmgTypes map[string]*damageType
	tagCache sync.Map // "registry#tag" -> map[string]bool
)

func ns(k string) string {
	if strings.Contains(k, ":") {
		return k
	}
	return "minecraft:" + k
}

func loadDamageTypes() {
	dmgTypes = map[string]*damageType{}
	p, err := wgdata.Load()
	if err != nil {
		return
	}
	for _, k := range p.Keys("damage_type") {
		raw, ok := p.JSON("damage_type", k)
		if !ok {
			continue
		}
		var j struct {
			Message    string  `json:"message_id"`
			Death      string  `json:"death_message_type"`
			Scaling    string  `json:"scaling"`
			Exhaustion float32 `json:"exhaustion"`
			Effects    string  `json:"effects"`
		}
		if json.Unmarshal(raw, &j) != nil {
			continue
		}
		key := ns(k)
		id, _ := regsync.Index("minecraft:damage_type", key)
		dmgTypes[key] = &damageType{key: key, id: id, message: j.Message, deathMsg: j.Death, scaling: j.Scaling, exhaustion: j.Exhaustion, effects: j.Effects, tags: map[string]bool{}}
	}
	for _, t := range p.TagKeys("damage_type") {
		ids, ok := p.Tag("damage_type", t)
		if !ok {
			continue
		}
		for _, id := range ids {
			if d := dmgTypes[ns(id)]; d != nil {
				d.tags[ns(t)] = true
			}
		}
	}
}

func damageTypeOf(key string) *damageType {
	dmgOnce.Do(loadDamageTypes)
	if d := dmgTypes[ns(key)]; d != nil {
		return d
	}
	return dmgTypes["minecraft:generic"]
}

// inTag reports whether name is in the data-pack tag reg/tag (e.g.
// "entity_type", "minecraft:sensitive_to_smite").
func inTag(reg, tag, name string) bool {
	tag = ns(strings.TrimPrefix(tag, "#"))
	k := reg + "#" + tag
	v, ok := tagCache.Load(k)
	if !ok {
		set := map[string]bool{}
		if p, err := wgdata.Load(); err == nil {
			if ids, ok := p.Tag(reg, tag); ok {
				for _, id := range ids {
					set[ns(id)] = true
				}
			}
		}
		v, _ = tagCache.LoadOrStore(k, set)
	}
	return v.(map[string]bool)[ns(name)]
}

// matchHolder matches name against "#tag", "id" or a list of either.
func matchHolder(reg string, raw json.RawMessage, name string) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if strings.HasPrefix(s, "#") {
			return inTag(reg, s, name)
		}
		return ns(s) == ns(name)
	}
	var l []string
	if json.Unmarshal(raw, &l) == nil {
		for _, x := range l {
			if ns(x) == ns(name) {
				return true
			}
		}
	}
	return false
}

func itemName(st item.Stack) string { return ns(item.NameOf(st.Item)) }

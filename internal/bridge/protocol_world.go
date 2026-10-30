package bridge

// Entities, effects, attributes, lists, particles and boss bars. These ride
// on OpQuery and OpAction (no new op codes); the range 80-99 stays reserved
// for this area.
//
//	entity: {uuid UUID, entityId int, type string, world string, x, y, z, yaw, pitch float,
//	         valid bool, living bool, health, maxHealth float}
//	effect: {type string, amplifier, duration int (-1 infinite), ambient, particles, icon bool}

// Event names fired from Go.
const (
	EventEntityDamage   = "EntityDamage"   // entity, damager entity | nil, direct entity | nil, cause string (damage type key), damage; mutated: damage
	EventPlayerDeath    = "PlayerDeath"    // player, deathMessage json, keepInventory, keepLevel bool, drops [{type, count}], level int; mutated: deathMessage json | hideMessage, keepInventory, clearDrops
	EventPlayerRespawn  = "PlayerRespawn"  // player, location; mutated: location
	EventPlayerTeleport = "PlayerTeleport" // player, from, to location, cause string; mutated: to
	EventEntitySpawn    = "EntitySpawn"    // entity, reason string (NATURAL | CHUNK_GEN | SPAWNER | DEFAULT)
)

// Query kinds.
const (
	QueryEntity       = "entity"        // uuid -> entity | nil
	QueryEntities     = "entities"      // world, [x, y, z, radius] -> [entity]
	QueryAttribute    = "attribute"     // uuid, name -> {has bool, base, value, default float, modifiers [{id, amount float, op int}]}
	QueryEffects      = "effects"       // uuid -> [effect]
	QueryList         = "list"          // list (ops | whitelist | profile | ip) -> [{target, name, reason, source, created, expires int (unix ms, 0 = never)}]; whitelist adds enabled via QueryListState
	QueryListState    = "list_state"    // -> {whitelist bool (enforced)}
	QueryHighest      = "highest"       // world, x, z -> y int
	QueryChunkLoaded  = "chunk_loaded"  // world, x, z -> bool
	QueryLoadedChunks = "loaded_chunks" // world -> [{x, z}]
	QuerySpawnEntity  = "spawn_entity"  // world, type, x, y, z, yaw, pitch -> entity
	QueryDropItem     = "drop_item"     // world, x, y, z, item, count, naturally bool -> entity
)

// Action kinds.
const (
	ActionAttributeBase = "attribute_base" // uuid, name, value
	ActionAttributeMod  = "attribute_mod"  // uuid, name, id, amount, op int, remove bool
	ActionEffectAdd     = "effect_add"     // uuid, type, amplifier, duration, ambient, particles, icon
	ActionEffectRemove  = "effect_remove"  // uuid, type ("" = all)
	ActionOp            = "op_set"         // uuid, name, value bool
	ActionWhitelist     = "whitelist_set"  // uuid, name, value bool
	ActionWhitelistOn   = "whitelist_on"   // value bool
	ActionBan           = "ban"            // list (profile | ip), target, name, reason, source, expires int (unix ms, 0 = never)
	ActionPardon        = "pardon"         // list, target
	ActionEntityRemove  = "entity_remove"  // uuid
	ActionParticle      = "particle"       // player | world, name, x, y, z, count, dx, dy, dz, speed, force bool, data {color int, scale, power, roll float, delay int, block string, toColor int}
	ActionBossBar       = "bossbar"        // player, id UUID, op (add | remove | progress | title | style | flags), title json, progress, color, overlay, flags int
)

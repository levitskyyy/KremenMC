package api

// Entity handles stay valid after removal; IsValid reports whether the entity
// is still in a world. Accessors on removed entities return their last state.
type Entity interface {
	EntityID() int32
	UUID() UUID
	Type() Key
	World() World
	Location() Location
	Velocity() Vec3
	SetVelocity(Vec3)
	OnGround() bool
	BoundingBox() (min, max Vec3)

	// Teleport moves the entity, possibly across worlds. It fires
	// PlayerTeleportEvent for players and reports false if cancelled.
	Teleport(to Location, cause TeleportCause) bool

	CustomName() (Component, bool)
	SetCustomName(name *Component)
	Silent() bool
	SetSilent(bool)
	Glowing() bool
	SetGlowing(bool)
	Invulnerable() bool
	SetInvulnerable(bool)
	Gravity() bool
	SetGravity(bool)

	Passengers() []Entity
	AddPassenger(Entity) bool
	RemovePassenger(Entity) bool
	Vehicle() (Entity, bool)

	Tags() []string
	AddTag(tag string) bool
	RemoveTag(tag string) bool

	TicksLived() int64
	IsValid() bool
	Remove()

	PersistentData() DataContainer
	// Scheduler returns the Folia-style scheduler that follows this entity
	// across regions.
	Scheduler() EntityScheduler
}

type LivingEntity interface {
	Entity
	Health() float64
	SetHealth(float64)
	MaxHealth() float64
	Damage(amount float64, source DamageSource)
	IsDead() bool
	EyeLocation() Location
	Equipment() Equipment
	AddEffect(PotionEffect) bool
	RemoveEffect(effect Key)
	Effects() []PotionEffect
}

type Equipment interface {
	Get(slot EquipmentSlot) ItemStack
	Set(slot EquipmentSlot, item ItemStack)
}

type EquipmentSlot uint8

const (
	SlotMainHand EquipmentSlot = iota
	SlotOffHand
	SlotFeet
	SlotLegs
	SlotChest
	SlotHead
	SlotBody
)

type PotionEffect struct {
	Type      Key
	Amplifier int
	Duration  Ticks // -1 for infinite
	Ambient   bool
	Particles bool
	Icon      bool
}

type DamageSource struct {
	Type     Key // damage type registry key, e.g. "minecraft:fall"
	Cause    DamageCause
	Direct   Entity // projectile or attacker; nil for environmental damage
	Causing  Entity // owner of a projectile; may equal Direct
	Position *Vec3
}

type TeleportCause uint8

const (
	TeleportPlugin TeleportCause = iota
	TeleportCommand
	TeleportEnderPearl
	TeleportChorusFruit
	TeleportNetherPortal
	TeleportEndPortal
	TeleportEndGateway
	TeleportSpectate
	TeleportDismount
	TeleportExitBed
	TeleportUnknown
)

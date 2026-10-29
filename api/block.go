package api

// BlockState is an immutable block state. Two states are equal iff their IDs
// are equal.
type BlockState interface {
	ID() uint32
	Type() Key
	Property(name string) (string, bool)
	Properties() map[string]string
	// With returns the state with one property changed, or the receiver if the
	// property or value is invalid.
	With(name, value string) BlockState
	IsAir() bool
	IsSolid() bool
	IsLiquid() bool
	// String formats as "minecraft:oak_log[axis=y]".
	String() string
}

// Block is a position handle in a world. Reads reflect current world state.
type Block interface {
	World() World
	Pos() BlockPos
	State() BlockState
	Type() Key
	SetState(state BlockState, flags BlockUpdateFlags) bool
	Relative(face BlockFace) Block
	LightLevel() uint8
	SkyLight() uint8
	BlockLight() uint8
	Biome() Key
	// BlockEntity returns raw block entity NBT (network form), if any.
	BlockEntity() ([]byte, bool)
	SetBlockEntity(nbt []byte) bool
	Break(dropItems bool) bool
	PersistentData() DataContainer
}

type BlockRegistry interface {
	// Parse accepts "oak_log", "minecraft:oak_log[axis=x]".
	Parse(s string) (BlockState, error)
	ByID(id uint32) (BlockState, bool)
	Default(block Key) (BlockState, bool)
	Air() BlockState
}

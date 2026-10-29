package api

type Dimension uint8

const (
	Overworld Dimension = iota
	Nether
	End
	CustomDimension
)

type World interface {
	Name() string
	Key() Key
	UUID() UUID
	Seed() int64
	Dimension() Dimension
	DimensionType() Key
	MinY() int
	MaxY() int // exclusive

	Spawn() Location
	SetSpawn(Location)
	Time() int64
	SetTime(int64)
	FullTime() int64
	Difficulty() Difficulty
	SetDifficulty(Difficulty)
	Storming() bool
	SetStorming(bool)
	Thundering() bool
	SetThundering(bool)
	GameRule(name string) (string, bool)
	SetGameRule(name, value string) bool

	// Block returns a handle; it does not load the chunk.
	Block(pos BlockPos) Block
	BlockState(pos BlockPos) BlockState
	// SetBlockState requires the owning region. Flags control physics and updates.
	SetBlockState(pos BlockPos, state BlockState, flags BlockUpdateFlags) bool
	HighestBlockY(x, z int) int
	Biome(pos BlockPos) Key

	IsChunkLoaded(x, z int) bool
	// LoadChunk loads or generates asynchronously; done runs on the owning region.
	LoadChunk(x, z int, generate bool, done func(ok bool))
	UnloadChunk(x, z int, save bool) bool
	LoadedChunks() []ChunkPos
	// AddChunkTicket keeps a chunk loaded until RemoveChunkTicket by the same owner.
	AddChunkTicket(owner string, x, z int)
	RemoveChunkTicket(owner string, x, z int)

	Players() []Player
	Entities() []Entity
	NearbyEntities(center Vec3, radius float64) []Entity
	Entity(id UUID) (Entity, bool)
	SpawnEntity(kind Key, at Location, configure func(Entity)) (Entity, error)
	DropItem(at Vec3, item ItemStack) (Entity, error)

	PlaySound(sound Sound)
	SpawnParticle(particle Key, at Vec3, count int, offset Vec3, speed float64)
	Explode(at Vec3, power float32, fire, breakBlocks bool)

	PersistentData() DataContainer
	Save() error
}

type BlockUpdateFlags uint8

const (
	UpdateNeighbors BlockUpdateFlags = 1 << iota
	UpdateClients
	UpdatePhysics
	UpdateDefault = UpdateNeighbors | UpdateClients | UpdatePhysics
)

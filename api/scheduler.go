package api

import "time"

// Task is a handle to a scheduled unit of work. Owner is the plugin id; all of
// a plugin's tasks are cancelled when it is disabled.
type Task interface {
	ID() uint64
	Owner() string
	Cancel()
	Cancelled() bool
	Repeating() bool
}

// TaskScheduler runs work on a specific tick context. Delay and period are in
// ticks; a delay of 0 runs on the next tick of that context.
type TaskScheduler interface {
	Run(owner string, fn func(Task)) Task
	RunDelayed(owner string, delay Ticks, fn func(Task)) Task
	RunRepeating(owner string, delay, period Ticks, fn func(Task)) Task
}

// EntityScheduler follows an entity between regions. retired runs instead of
// fn if the entity is removed before fn executes; it may be nil.
type EntityScheduler interface {
	TaskScheduler
	RunWithRetired(owner string, delay Ticks, fn func(Task), retired func()) Task
}

// AsyncScheduler runs work off the tick goroutines on a shared pool.
type AsyncScheduler interface {
	RunNow(owner string, fn func(Task)) Task
	RunDelayed(owner string, delay time.Duration, fn func(Task)) Task
	RunAtFixedRate(owner string, delay, period time.Duration, fn func(Task)) Task
}

// Scheduler maps to both the Bukkit scheduler (Global + Async) and the Folia
// schedulers (GlobalRegion, Region, Entity, Async).
type Scheduler interface {
	// Global runs on the server-wide tick, which owns world-independent state
	// (time, weather, plugin bookkeeping) but not blocks or entities.
	Global() TaskScheduler
	// Region runs on whichever worker owns the given chunk.
	Region(world World, chunkX, chunkZ int) TaskScheduler
	Entity(e Entity) EntityScheduler
	Async() AsyncScheduler
	CancelAll(owner string)
	// IsOwnedByCurrentRegion reports whether the calling goroutine may touch
	// the given chunk right now.
	IsOwnedByCurrentRegion(world World, chunkX, chunkZ int) bool
	IsGlobalThread() bool
}

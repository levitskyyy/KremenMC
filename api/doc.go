// Package api is the stable plugin-facing API of kremenmc. It contains interfaces
// and plain value types only; the server provides the implementations.
//
// Threading follows the Folia model: world state (blocks, entities, chunks) is
// owned by regions and may only be touched from the owning region's tick or
// from tasks scheduled on Scheduler.Region / Scheduler.Entity. Methods that are
// safe from any goroutine say so.
package api

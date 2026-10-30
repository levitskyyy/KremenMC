package world

import "github.com/levitskyyy/KremenMC/internal/nbt"

// HasGenEntities reports whether generation left entities in c that the
// live world has not taken yet.
func (c *Chunk) HasGenEntities() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.GenEntities) > 0
}

// TakeGenEntities removes and returns the generated entities of c that
// take accepts, once: the chunk is marked dirty so the next save no longer
// carries them. The others stay with the chunk.
func (c *Chunk) TakeGenEntities(take func(nbt.Compound) bool) []nbt.Compound {
	c.mu.Lock()
	var out, keep []nbt.Compound
	for _, e := range c.GenEntities {
		if take(e) {
			out = append(out, e)
		} else {
			keep = append(keep, e)
		}
	}
	c.GenEntities = keep
	c.mu.Unlock()
	if len(out) > 0 {
		c.MarkDirty()
	}
	return out
}

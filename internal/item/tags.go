package item

import (
	"strings"
	"sync"

	"github.com/levitskyyy/KremenMC/internal/net/regsync"
)

var (
	tagOnce sync.Once
	tagSets map[string]map[int32]bool
)

// tagContains reports whether network tag (registry, name) holds id.
func tagContains(reg, name string, id int32) bool {
	tagOnce.Do(func() {
		tagSets = map[string]map[int32]bool{}
		for _, g := range regsync.Tags() {
			for _, t := range g.Tags {
				m := make(map[int32]bool, len(t.IDs))
				for _, x := range t.IDs {
					m[x] = true
				}
				tagSets[g.Registry+"#"+t.Name] = m
			}
		}
	})
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	return tagSets[reg+"#"+name][id]
}

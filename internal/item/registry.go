package item

import (
	"strings"

	"github.com/levitskyyy/KremenMC/internal/data"
)

func numItems() int { return len(data.Canonical().Items) }

// IDOf resolves an item name with or without namespace.
func IDOf(name string) (int32, bool) {
	id, ok := data.Canonical().ItemByName(strings.TrimPrefix(name, "minecraft:"))
	return int32(id), ok
}

// NameOf returns the namespaced item name, or "" for unknown ids.
func NameOf(id int32) string {
	n := data.Canonical().ItemName(data.ItemID(id))
	if n == "" {
		return ""
	}
	return "minecraft:" + n
}

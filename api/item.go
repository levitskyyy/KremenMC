package api

// ItemStack is a value type. Components holds data components not modelled
// explicitly, keyed by component id, as protocol 777 wire values (the
// component's stream encoding); Removed lists default components the stack
// explicitly removes.
type ItemStack struct {
	Type            Key
	Count           int32
	Name            *Component
	Lore            []Component
	Damage          int32
	Unbreakable     bool
	CustomModelData []float32
	Enchantments    map[Key]int32
	Components      map[Key][]byte
	Removed         []Key
}

func Item(kind Key, count int32) ItemStack { return ItemStack{Type: kind, Count: count} }

func (s ItemStack) IsEmpty() bool {
	return s.Count <= 0 || s.Type == "" || s.Type.Normalize() == "minecraft:air"
}

// Similar reports whether two stacks are the same item ignoring count.
func (s ItemStack) Similar(o ItemStack) bool {
	if s.Type.Normalize() != o.Type.Normalize() || s.Damage != o.Damage || s.Unbreakable != o.Unbreakable {
		return false
	}
	if (s.Name == nil) != (o.Name == nil) || s.Name != nil && s.Name.Plain() != o.Name.Plain() {
		return false
	}
	if len(s.Lore) != len(o.Lore) || len(s.Enchantments) != len(o.Enchantments) || len(s.Components) != len(o.Components) || len(s.Removed) != len(o.Removed) {
		return false
	}
	for i := range s.Lore {
		if s.Lore[i].Plain() != o.Lore[i].Plain() {
			return false
		}
	}
	for k, v := range s.Enchantments {
		if o.Enchantments[k] != v {
			return false
		}
	}
	for k, v := range s.Components {
		if w, ok := o.Components[k]; !ok || string(w) != string(v) {
			return false
		}
	}
	for _, k := range s.Removed {
		found := false
		for _, x := range o.Removed {
			found = found || x == k
		}
		if !found {
			return false
		}
	}
	return true
}

type ItemRegistry interface {
	MaxStackSize(kind Key) int32
	Exists(kind Key) bool
	ID(kind Key) (int32, bool)
	ByID(id int32) (Key, bool)
}

type InventoryType uint8

const (
	InvChest InventoryType = iota
	InvPlayer
	InvCrafting
	InvWorkbench
	InvFurnace
	InvDispenser
	InvHopper
	InvAnvil
	InvEnchanting
	InvBrewing
	InvBeacon
	InvShulkerBox
	InvBarrel
	InvSmoker
	InvBlastFurnace
	InvGrindstone
	InvStonecutter
	InvLoom
	InvCartography
	InvSmithing
	InvMerchant
	InvCrafter
)

type Inventory interface {
	Type() InventoryType
	Title() Component
	Size() int
	Item(slot int) ItemStack
	SetItem(slot int, item ItemStack)
	Contents() []ItemStack
	SetContents([]ItemStack)
	// AddItem merges into existing stacks then empty slots; returns what did not fit.
	AddItem(items ...ItemStack) []ItemStack
	RemoveItem(items ...ItemStack) []ItemStack
	Contains(kind Key, minCount int32) bool
	FirstEmpty() int
	Clear()
	Viewers() []Player
	// Holder is the block or entity owning the inventory, nil for custom ones.
	Holder() any
}

type PlayerInventory interface {
	Inventory
	HeldSlot() int
	SetHeldSlot(int)
	ItemInHand(hand Hand) ItemStack
	SetItemInHand(hand Hand, item ItemStack)
	Armor() [4]ItemStack // feet, legs, chest, head
	SetArmor([4]ItemStack)
	Cursor() ItemStack
	SetCursor(ItemStack)
}

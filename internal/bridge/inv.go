package bridge

import (
	"context"
	"fmt"
)

// Inventory and item operations (op range 60-79).
//
// An inventory ref is a player UUID (the player's inventory, Bukkit
// PlayerInventory indices: 0-8 hotbar, 9-35 main, 36-39 feet..head, 40 off
// hand), an int (a custom inventory created with OpInvCreate), or the string
// "ender:<uuid>" (that player's ender chest).
//
// Item map: {id string, count int, c {component name: value}, rm [component names]}.
// Component values are raw protocol 777 bytes, except
//
//	custom_name, item_name                string (component json)
//	lore                                  [string (component json)]
//	enchantments, stored_enchantments     {enchantment key: level}
//	damage, max_damage, max_stack_size,
//	repair_cost, dyed_color               int
//	unbreakable, glider                   true
//	enchantment_glint_override            bool
//	item_model                            string (key)
//	tooltip_display                       {hide bool, hidden [component names]}
//	custom_model_data                     {floats [float], flags [bool], strings [string], colors [int]}
//	profile                               {name string, uuid UUID, properties [{name, value, signature}]}
//
// custom_data stays raw: network NBT, nameless root compound.
const (
	OpInvContents uint16 = 60 // call   [ref] -> [list of item | nil, held slot int, cursor item | nil]
	OpInvSet      uint16 = 61 // notify [ref, slot, item | nil]; slot -1 on a player ref is the cursor
	OpInvSetAll   uint16 = 62 // notify [ref, list of item | nil]
	OpInvCreate   uint16 = 63 // call   [type string, size int, title json] -> [id int]
	OpInvFree     uint16 = 64 // notify [id]
	OpInvOpen     uint16 = 65 // notify [player uuid, ref]
	OpInvClose    uint16 = 66 // notify [player uuid]
	OpInvView     uint16 = 67 // call   [player uuid] -> [{ref, type, size, title json} | nil]
	OpInvHeld     uint16 = 68 // notify [player uuid, slot]
	OpInvAdd      uint16 = 69 // call   [ref, list of item] -> [list of leftover item]

	// OpInvEvent is initiated by Go: an inventory event for Java plugins.
	//
	//	InventoryClick: player, ref (top inventory), type, size, title, raw slot, click string,
	//	                button, mode, hotbar, current item, cursor item
	//	                -> cancelled, out {cursor, current: item} (set by plugins)
	//	InventoryDrag:  player, ref, type, size, title, slots [raw], items [item], cursor (old),
	//	                newCursor, right bool -> cancelled
	//	InventoryOpen:  player, ref, type, size, title -> cancelled
	//	InventoryClose: player, ref, type, size, title, reason
	OpInvEvent uint16 = 70 // call   [event name, fields map] -> [cancelled bool, out map]

	OpItemInfo uint16 = 71 // call   [item id] -> [{exists bool, maxStack, maxDamage int}]

	opInvFirst = 60
	opInvLast  = 79
)

// InvHost is implemented by a Host that serves the inventory operations.
type InvHost interface {
	InvCall(op uint16, args []any) ([]any, error)
	InvNotify(op uint16, args []any)
}

func isInvOp(op uint16) bool { return op >= opInvFirst && op <= opInvLast }

func (b *Bridge) invNotify(op uint16, args []any) {
	if h, ok := b.host.(InvHost); ok {
		h.InvNotify(op, args)
	}
}

func (b *Bridge) invCall(op uint16, args []any) ([]any, error) {
	if h, ok := b.host.(InvHost); ok {
		return h.InvCall(op, args)
	}
	return nil, fmt.Errorf("inventory op %d not supported", op)
}

// InvEvent fires an inventory event (OpInvEvent) with the sync event budget.
func (b *Bridge) InvEvent(ctx context.Context, name string, f map[string]any) (cancelled bool, out Fields) {
	c := b.conn.Load()
	if c == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, b.cfg.EventBudget)
	defer cancel()
	res, err := c.Call(ctx, OpInvEvent, name, f)
	if err != nil {
		b.warnSlow("inventory event failed", "event", name, "err", err)
		return false, nil
	}
	return ArgBool(res, 0), Fields(ArgMap(res, 1))
}

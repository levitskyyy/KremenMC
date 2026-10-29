package main

// schemaFixes correct minecraft-data mistakes, verified against the 26.3
// server stream codecs. Each applies only while the field still has From.
var schemaFixes = []fieldFix{
	{SD: "play.toClient", Packet: "initialize_world_border", Field: "speed", From: `"varint"`, To: `"varlong"`,
		Note: "lerp time is VarLong"},
	{SD: "play.toClient", Packet: "world_border_lerp_size", Field: "speed", From: `"varint"`, To: `"varlong"`,
		Note: "lerp time is VarLong"},
	{SD: "play.toClient", Packet: "multi_block_change", Field: "records",
		From: `["array",{"countType":"varint","type":"varint"}]`, To: `["array",{"countType":"varint","type":"varlong"}]`,
		Note: "block entries are VarLong"},
	{SD: "play.toServer", Packet: "update_structure_block", Field: "seed", From: `"varint"`, To: `"varlong"`,
		Note: "seed is VarLong"},
	{SD: "play.toClient", Packet: "move_minecart", Field: "steps",
		From: `["array",{"countType":"varint","type":["container",[{"name":"position","type":"vec3f"},{"name":"velocity","type":"vec3f"},{"name":"yaw","type":"f32"},{"name":"pitch","type":"f32"},{"name":"weight","type":"f32"}]]}]`,
		To:   `["array",{"countType":"varint","type":["container",[{"name":"position","type":"vec3f64"},{"name":"velocity","type":"vec3f64"},{"name":"yaw","type":"i8"},{"name":"pitch","type":"i8"},{"name":"weight","type":"f32"}]]}]`,
		Note: "MinecartStep uses Vec3 (doubles) and rotation bytes"},
	{SD: "play.toClient", Packet: "player_info", Field: "action",
		From: `["bitflags",{"type":"u8","flags":["add_player","initialize_chat","update_game_mode","update_listed","update_latency","update_display_name","update_hat","update_list_order"]}]`,
		To:   `["bitflags",{"type":"u8","flags":["add_player","initialize_chat","update_game_mode","update_listed","update_latency","update_display_name","update_list_order","update_hat"]}]`,
		Note: "action bits follow enum order: list order 0x40, hat 0x80"},
	{SD: "play.toClient", Packet: "player_info", Field: "data[].listed",
		From: `["switch",{"compareTo":"../action/update_listed","fields":{"true":"varint"},"default":"void"}]`,
		To:   `["switch",{"compareTo":"../action/update_listed","fields":{"true":"bool"},"default":"void"}]`,
		Note: "listed is a boolean"},
	{SD: "", Packet: "packet_common_custom_click_action", Field: "nbt",
		From: `["option","anonymousNbt"]`, To: `["buffer",{"countType":"varint"}]`,
		Note: "payload is a length-prefixed (max 65536) optional NBT tag"},
}

// opaqueTypes are never decoded; a packet field using one (and every field
// after it) is kept as raw bytes.
var opaqueTypes = map[string]bool{
	"Slot":                   true,
	"UntrustedSlot":          true,
	"HashedSlot":             true,
	"ItemStackTemplate":      true,
	"SlotComponent":          true,
	"UntrustedSlotComponent": true,
	"Particle":               true,
	"entityMetadata":         true,
	"command_node":           true,
	"SlotDisplay":            true,
	"RecipeDisplay":          true,
}

// rawNBTFields lists anonymousNbt fields that are not text components, keyed
// by owner (packet or named type) and field name. All other anonymousNbt
// fields decode as proto.Text.
var rawNBTFields = map[string]bool{
	"registry_data.value":                       true,
	"show_dialog.dialog":                        true,
	"packet_common_show_dialog.dialog":          true,
	"ChatType.style":                            true,
	"scoreboard_objective.styling":              true,
	"scoreboard_score.styling":                  true,
	"packet_common_custom_click_action.nbt":     true,
	"packet_common_custom_report_details.value": true,
}

// nativeTypes maps protodef native names to hand-written proto types with
// Encode/Decode methods.
var nativeTypes = map[string]string{
	"lpVec3":          "proto.LpVec3",
	"anonOptionalNbt": "proto.RawNBT",
	"entityMoveDelta": "proto.MoveDelta",
}

// fieldRenames gives Go names to fields that would collide with the ID method.
var fieldRenames = map[string]string{
	"ping.id":                   "Payload",
	"pong.id":                   "Payload",
	"ping_request.id":           "Payload",
	"ping_response.id":          "Payload",
	"registry_data.id":          "Registry",
	"custom_click_action.id":    "ActionID",
	"hide_message.id":           "MessageID",
	"select_advancement_tab.id": "Tab",
	"set_projectile_power.id":   "EntityID",
}

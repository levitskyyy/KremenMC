package main

// patch777 turns the minecraft-data 26.1 (775) schema into 26.3 (777).
// Sources: minecraft.wiki "Java Edition protocol/Packets" rev 3865877 (26.3),
// Protocol History (26.2), and the packet registration order / stream codecs
// of the official 26.3 server.jar (GameProtocols, ConfigurationProtocols,
// LoginProtocols), inspected with javap.
var patch777 = &patch{
	Protocol: 777,
	Version:  "26.3",
	Ops: []patchOp{
		{
			SD: "login.toClient", Op: "replace", Packet: "success",
			Note: "26.2: session ID UUID appended after the game profile",
			Def: `["container",[
				{"name":"uuid","type":"UUID"},
				{"name":"username","type":"string"},
				{"name":"properties","type":["array",{"countType":"varint","type":"GameProfileProperty"}]},
				{"name":"sessionId","type":"UUID"}]]`,
		},
		{
			SD: "configuration.toClient", Op: "insert", Packet: "post_effects", After: "add_resource_pack", Mojang: "post_effects",
			Note: "26.3: new packet, IDs after 0x0A shift by one",
			Def:  `["container",[{"name":"effects","type":["array",{"countType":"varint","type":"string"}]}]]`,
		},
		{
			SD: "play.toClient", Op: "type", Packet: "SpawnInfo",
			Note: "26.3: game mode and previous game mode are VarInt enums; previous uses 0 = none, else id+1",
			Def: `["container",[
				{"name":"dimension","type":"varint"},
				{"name":"name","type":"string"},
				{"name":"hashedSeed","type":"i64"},
				{"name":"gamemode","type":["mapper",{"type":"varint","mappings":{"0":"survival","1":"creative","2":"adventure","3":"spectator"}}]},
				{"name":"previousGamemode","type":"varint"},
				{"name":"isDebug","type":"bool"},
				{"name":"isFlat","type":"bool"},
				{"name":"death","type":["option","GlobalPos"]},
				{"name":"portalCooldown","type":"varint"},
				{"name":"seaLevel","type":"varint"}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "login",
			Note: "26.2+: online mode boolean before enforces secure chat",
			Def: `["container",[
				{"name":"entityId","type":"i32"},
				{"name":"isHardcore","type":"bool"},
				{"name":"worldNames","type":["array",{"countType":"varint","type":"string"}]},
				{"name":"maxPlayers","type":"varint"},
				{"name":"viewDistance","type":"varint"},
				{"name":"simulationDistance","type":"varint"},
				{"name":"reducedDebugInfo","type":"bool"},
				{"name":"enableRespawnScreen","type":"bool"},
				{"name":"doLimitedCrafting","type":"bool"},
				{"name":"worldState","type":"SpawnInfo"},
				{"name":"onlineMode","type":"bool"},
				{"name":"enforcesSecureChat","type":"bool"}]]`,
		},
		{
			SD: "play.toClient", Op: "type", Packet: "PositionStep",
			Def: `["container",[{"name":"x","type":"f64"},{"name":"y","type":"f64"},{"name":"z","type":"f64"},{"name":"tickOffset","type":"varint"}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "sync_entity_position",
			Note: "26.3: velocity removed; position is a PositionPath (0 linear: Vec3, 1 stepped: list of steps)",
			Def: `["container",[
				{"name":"entityId","type":"varint"},
				{"name":"pathType","type":["mapper",{"type":"varint","mappings":{"0":"linear","1":"stepped"}}]},
				{"name":"position","type":["switch",{"compareTo":"pathType","fields":{"linear":"vec3f64"},"default":"void"}]},
				{"name":"steps","type":["switch",{"compareTo":"pathType","fields":{"stepped":["array",{"countType":"varint","type":"PositionStep"}]},"default":"void"}]},
				{"name":"yaw","type":"f32"},
				{"name":"pitch","type":"f32"},
				{"name":"onGround","type":"bool"}]]`,
		},
		{
			SD: "", Op: "type", Packet: "entityMoveDelta", Def: `"native"`,
			Note: "26.3: properties VarInt (bit0 on ground, rest step count) + linear or stepped deltas",
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "rel_entity_move",
			Note: "26.3: on ground moved into properties VarInt; stepped deltas",
			Def:  `["container",[{"name":"entityId","type":"varint"},{"name":"delta","type":"entityMoveDelta"}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "entity_move_look",
			Note: "26.3: as rel_entity_move, rotation after the delta",
			Def: `["container",[
				{"name":"entityId","type":"varint"},
				{"name":"delta","type":"entityMoveDelta"},
				{"name":"yaw","type":"i8"},
				{"name":"pitch","type":"i8"}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "entity_look",
			Note: "26.3: on ground moved before yaw",
			Def: `["container",[
				{"name":"entityId","type":"varint"},
				{"name":"onGround","type":"bool"},
				{"name":"yaw","type":"i8"},
				{"name":"pitch","type":"i8"}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "open_sign_entity",
			Note: "26.3: is-front boolean replaced by SignTextSlot VarInt (0 back, 1 front)",
			Def: `["container",[
				{"name":"location","type":"position"},
				{"name":"textSlot","type":["mapper",{"type":"varint","mappings":{"0":"back","1":"front"}}]}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "explosion",
			Note: "26.3: play sound boolean appended",
			Def: `["container",[
				{"name":"center","type":"vec3f64"},
				{"name":"radius","type":"f32"},
				{"name":"blockCount","type":"i32"},
				{"name":"playerKnockback","type":["option","vec3f64"]},
				{"name":"explosionParticle","type":"Particle"},
				{"name":"sound","type":"ItemSoundHolder"},
				{"name":"blockParticles","type":["array",{"countType":"varint","type":"ExplosionParticleEntry"}]},
				{"name":"playSound","type":"bool"}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "world_particles",
			Note: "26.3: particle first, per-axis max speed, VarInt count, randomization type",
			Def: `["container",[
				{"name":"particle","type":"Particle"},
				{"name":"overrideLimiter","type":"bool"},
				{"name":"alwaysShow","type":"bool"},
				{"name":"x","type":"f64"},
				{"name":"y","type":"f64"},
				{"name":"z","type":"f64"},
				{"name":"offsetX","type":"f32"},
				{"name":"offsetY","type":"f32"},
				{"name":"offsetZ","type":"f32"},
				{"name":"maxSpeedX","type":"f32"},
				{"name":"maxSpeedY","type":"f32"},
				{"name":"maxSpeedZ","type":"f32"},
				{"name":"count","type":"varint"},
				{"name":"randomization","type":["mapper",{"type":"varint","mappings":{"0":"default","1":"alternative","2":"alternative_with_speed"}}]}]]`,
		},
		{
			SD: "play.toClient", Op: "insert", Packet: "add_transient_block", After: "explosion", Mojang: "add_transient_block",
			Note: "26.3: new packet",
			Def:  `["container",[{"name":"location","type":"position"},{"name":"blockState","type":"varint"}]]`,
		},
		{
			SD: "play.toClient", Op: "insert", Packet: "post_effects", After: "add_resource_pack", Mojang: "post_effects",
			Note: "26.3: new packet",
			Def:  `["container",[{"name":"effects","type":["array",{"countType":"varint","type":"string"}]}]]`,
		},
		{
			SD: "play.toClient", Op: "insert", Packet: "swing_animation", After: "store_cookie", Mojang: "swing_animation",
			Note: "26.3: new packet; arm swings moved out of Entity Animation",
			Def: `["container",[
				{"name":"entityId","type":"varint"},
				{"name":"hand","type":["mapper",{"type":"varint","mappings":{"0":"main_hand","1":"off_hand"}}]},
				{"name":"animation","type":["mapper",{"type":"varint","mappings":{"0":"none","1":"whack","2":"stab"}}]},
				{"name":"duration","type":"varint"}]]`,
		},
		{
			SD: "play.toClient", Op: "type", Packet: "TeamParameters",
			Def: `["container",[
				{"name":"name","type":"anonymousNbt"},
				{"name":"prefix","type":"anonymousNbt"},
				{"name":"suffix","type":"anonymousNbt"},
				{"name":"nameTagVisibility","type":["mapper",{"type":"varint","mappings":{"0":"always","1":"never","2":"hide_for_other_teams","3":"hide_for_own_team"}}]},
				{"name":"collisionRule","type":["mapper",{"type":"varint","mappings":{"0":"always","1":"never","2":"push_other_teams","3":"push_own_team"}}]},
				{"name":"color","type":["option","varint"]},
				{"name":"flags","type":["bitflags",{"type":"u8","flags":["friendly_fire","see_friendly_invisible"]}]}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "teams",
			Note: "26.2+: parameters reordered (display name, prefix, suffix, visibility, collision, optional color, flags)",
			Def: `["container",[
				{"name":"team","type":"string"},
				{"name":"mode","type":["mapper",{"type":"i8","mappings":{"0":"add","1":"remove","2":"change","3":"join","4":"leave"}}]},
				{"name":"parameters","type":["switch",{"compareTo":"mode","fields":{"add":"TeamParameters","change":"TeamParameters"},"default":"void"}]},
				{"name":"players","type":["switch",{"compareTo":"mode","fields":{"add":["array",{"countType":"varint","type":"string"}],"join":["array",{"countType":"varint","type":"string"}],"leave":["array",{"countType":"varint","type":"string"}]},"default":"void"}]}]]`,
		},
		{
			SD: "play.toServer", Op: "replace", Packet: "teleport_confirm",
			Note: "26.3: resulting position and rotation appended",
			Def: `["container",[
				{"name":"teleportId","type":"varint"},
				{"name":"x","type":"f64"},
				{"name":"y","type":"f64"},
				{"name":"z","type":"f64"},
				{"name":"yaw","type":"f32"},
				{"name":"pitch","type":"f32"}]]`,
		},
		{
			SD: "play.toServer", Op: "insert", Packet: "punch", After: "pong", Mojang: "punch",
			Note: "26.3: new packet, no fields",
			Def:  `["container",[]]`,
		},
		{
			SD: "play.toServer", Op: "remove", Packet: "arm_animation",
			Note: "26.3: serverbound swing removed",
		},
		{
			SD: "play.toServer", Op: "replace", Packet: "update_sign",
			Note: "26.3: is-front boolean replaced by SignTextSlot VarInt after the lines",
			Def: `["container",[
				{"name":"location","type":"position"},
				{"name":"text1","type":"string"},
				{"name":"text2","type":"string"},
				{"name":"text3","type":"string"},
				{"name":"text4","type":"string"},
				{"name":"textSlot","type":["mapper",{"type":"varint","mappings":{"0":"back","1":"front"}}]}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "map_chunk",
			Note: "26.3: light masks use ByteBufCodecs.BIT_SET, a length-prefixed BitSet.toByteArray, not a long array",
			Def:  `["container",[{"name":"x","type":"i32"},{"name":"z","type":"i32"},{"name":"heightmaps","type":["array",{"countType":"varint","type":["container",[{"name":"type","type":["mapper",{"type":"varint","mappings":{"0":"world_surface_wg","1":"world_surface","2":"ocean_floor_wg","3":"ocean_floor","4":"motion_blocking","5":"motion_blocking_no_leaves"}}]},{"name":"data","type":["array",{"countType":"varint","type":"i64"}]}]]}]},{"name":"chunkData","type":"ByteArray"},{"name":"blockEntities","type":["array",{"countType":"varint","type":"chunkBlockEntity"}]},{"name":"skyLightMask","type":"ByteArray"},{"name":"blockLightMask","type":"ByteArray"},{"name":"emptySkyLightMask","type":"ByteArray"},{"name":"emptyBlockLightMask","type":"ByteArray"},{"name":"skyLight","type":["array",{"countType":"varint","type":["array",{"countType":"varint","type":"u8"}]}]},{"name":"blockLight","type":["array",{"countType":"varint","type":["array",{"countType":"varint","type":"u8"}]}]}]]`,
		},
		{
			SD: "play.toClient", Op: "replace", Packet: "update_light",
			Note: "26.3: light masks use ByteBufCodecs.BIT_SET, a length-prefixed BitSet.toByteArray, not a long array",
			Def:  `["container",[{"name":"chunkX","type":"varint"},{"name":"chunkZ","type":"varint"},{"name":"skyLightMask","type":"ByteArray"},{"name":"blockLightMask","type":"ByteArray"},{"name":"emptySkyLightMask","type":"ByteArray"},{"name":"emptyBlockLightMask","type":"ByteArray"},{"name":"skyLight","type":["array",{"countType":"varint","type":["array",{"countType":"varint","type":"u8"}]}]},{"name":"blockLight","type":["array",{"countType":"varint","type":["array",{"countType":"varint","type":"u8"}]}]}]]`,
		},
	},
}

// expectedOrder777 is the official 26.3 packet order (from the server's
// protocol builders). The generator fails if the patched schema disagrees.
var expectedOrder777 = map[string][]string{
	"handshaking.toServer":   {"intention"},
	"status.toClient":        {"status_response", "pong_response"},
	"status.toServer":        {"status_request", "ping_request"},
	"login.toClient":         {"login_disconnect", "hello", "login_finished", "login_compression", "custom_query", "cookie_request"},
	"login.toServer":         {"hello", "key", "custom_query_answer", "login_acknowledged", "cookie_response"},
	"configuration.toClient": {"cookie_request", "custom_payload", "disconnect", "finish_configuration", "keep_alive", "ping", "reset_chat", "registry_data", "resource_pack_pop", "resource_pack_push", "post_effects", "store_cookie", "transfer", "update_enabled_features", "update_tags", "select_known_packs", "custom_report_details", "server_links", "clear_dialog", "show_dialog", "code_of_conduct"},
	"configuration.toServer": {"client_information", "cookie_response", "custom_payload", "finish_configuration", "keep_alive", "pong", "resource_pack", "select_known_packs", "custom_click_action", "accept_code_of_conduct"},
	"play.toClient":          {"bundle_delimiter", "add_entity", "animate", "award_stats", "block_changed_ack", "block_destruction", "block_entity_data", "block_event", "block_update", "boss_event", "change_difficulty", "chunk_batch_finished", "chunk_batch_start", "chunks_biomes", "clear_titles", "command_suggestions", "commands", "container_close", "container_set_content", "container_set_data", "container_set_slot", "cookie_request", "cooldown", "custom_chat_completions", "custom_payload", "damage_event", "debug_block_value", "debug_chunk_value", "debug_entity_value", "debug_event", "debug_sample", "delete_chat", "disconnect", "disguised_chat", "entity_event", "entity_position_sync", "explode", "add_transient_block", "forget_level_chunk", "game_event", "game_rule_values", "game_test_highlight_pos", "mount_screen_open", "hurt_animation", "initialize_border", "keep_alive", "level_chunk_with_light", "level_event", "level_particles", "light_update", "login", "low_disk_space_warning", "map_item_data", "merchant_offers", "move_entity_pos", "move_entity_pos_rot", "move_minecart_along_track", "move_entity_rot", "move_vehicle", "open_book", "open_screen", "open_sign_editor", "ping", "pong_response", "place_ghost_recipe", "player_abilities", "player_chat", "player_combat_end", "player_combat_enter", "player_combat_kill", "player_info_remove", "player_info_update", "player_look_at", "player_position", "player_rotation", "recipe_book_add", "recipe_book_remove", "recipe_book_settings", "remove_entities", "remove_mob_effect", "reset_score", "resource_pack_pop", "resource_pack_push", "post_effects", "respawn", "rotate_head", "section_blocks_update", "select_advancements_tab", "server_data", "set_action_bar_text", "set_border_center", "set_border_lerp_size", "set_border_size", "set_border_warning_delay", "set_border_warning_distance", "set_camera", "set_chunk_cache_center", "set_chunk_cache_radius", "set_cursor_item", "set_default_spawn_position", "set_display_objective", "set_entity_data", "set_entity_link", "set_entity_motion", "set_equipment", "set_experience", "set_health", "set_held_slot", "set_objective", "set_passengers", "set_player_inventory", "set_player_team", "set_score", "set_simulation_distance", "set_subtitle_text", "set_time", "set_title_text", "set_titles_animation", "sound_entity", "sound", "start_configuration", "stop_sound", "store_cookie", "swing_animation", "system_chat", "tab_list", "tag_query", "take_item_entity", "teleport_entity", "test_instance_block_status", "ticking_state", "ticking_step", "transfer", "update_advancements", "update_attributes", "update_mob_effect", "update_recipes", "update_tags", "projectile_power", "custom_report_details", "server_links", "waypoint", "clear_dialog", "show_dialog"},
	"play.toServer":          {"accept_teleportation", "attack", "block_entity_tag_query", "bundle_item_selected", "change_difficulty", "change_game_mode", "chat_ack", "chat_command", "chat_command_signed", "chat", "chat_session_update", "chunk_batch_received", "client_command", "client_tick_end", "client_information", "command_suggestion", "configuration_acknowledged", "container_button_click", "container_click", "container_close", "container_slot_state_changed", "cookie_response", "custom_payload", "debug_subscription_request", "edit_book", "entity_tag_query", "interact", "jigsaw_generate", "keep_alive", "lock_difficulty", "move_player_pos", "move_player_pos_rot", "move_player_rot", "move_player_status_only", "move_vehicle", "paddle_boat", "pick_item_from_block", "pick_item_from_entity", "ping_request", "place_recipe", "player_abilities", "player_action", "player_command", "player_input", "player_loaded", "pong", "punch", "recipe_book_change_settings", "recipe_book_seen_recipe", "rename_item", "resource_pack", "seen_advancements", "select_trade", "set_beacon", "set_carried_item", "set_command_block", "set_command_minecart", "set_creative_mode_slot", "set_game_rule", "set_jigsaw_block", "set_structure_block", "set_test_block", "sign_update", "spectator_action", "teleport_to_entity", "test_instance_block_action", "use_item_on", "use_item", "custom_click_action"},
}

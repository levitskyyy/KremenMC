package command

import "github.com/levitskyyy/KremenMC/api"

// Parsers777 is the minecraft:command_argument_type registry of Java 26.3
// (protocol 777) in network-ID order, taken from the registration order in
// net.minecraft.commands.synchronization.ArgumentTypeInfos.bootstrap of the
// official 26.3 server.jar. Older protocols supply their own list to
// NewParserTable.
var Parsers777 = []api.ParserID{
	"brigadier:bool",
	"brigadier:float",
	"brigadier:double",
	"brigadier:integer",
	"brigadier:long",
	"brigadier:string",
	"minecraft:entity",
	"minecraft:game_profile",
	"minecraft:block_pos",
	"minecraft:column_pos",
	"minecraft:vec3",
	"minecraft:vec2",
	"minecraft:block_state",
	"minecraft:block_predicate",
	"minecraft:item_stack",
	"minecraft:item_predicate",
	"minecraft:team_color",
	"minecraft:hex_color",
	"minecraft:component",
	"minecraft:style",
	"minecraft:message",
	"minecraft:nbt_compound_tag",
	"minecraft:nbt_tag",
	"minecraft:nbt_path",
	"minecraft:objective",
	"minecraft:objective_criteria",
	"minecraft:operation",
	"minecraft:particle",
	"minecraft:angle",
	"minecraft:rotation",
	"minecraft:scoreboard_slot",
	"minecraft:score_holder",
	"minecraft:swizzle",
	"minecraft:team",
	"minecraft:item_slot",
	"minecraft:item_slots",
	"minecraft:resource_location",
	"minecraft:function",
	"minecraft:entity_anchor",
	"minecraft:int_range",
	"minecraft:float_range",
	"minecraft:dimension",
	"minecraft:gamemode",
	"minecraft:time",
	"minecraft:resource_or_tag",
	"minecraft:resource_or_tag_key",
	"minecraft:resource",
	"minecraft:resource_key",
	"minecraft:resource_selector",
	"minecraft:template_mirror",
	"minecraft:template_rotation",
	"minecraft:heightmap",
	"minecraft:loot_table",
	"minecraft:loot_predicate",
	"minecraft:loot_modifier",
	"minecraft:context_float_provider",
	"minecraft:context_int_provider",
	"minecraft:slot_source",
	"minecraft:dialog",
	"minecraft:feature",
	"minecraft:swing_animation",
	"minecraft:uuid",
}

// Parser IDs used by kremenmc that are not constants in api.
const (
	ParserBlockPredicate api.ParserID = "minecraft:block_predicate"
	ParserItemPredicate  api.ParserID = "minecraft:item_predicate"
	ParserAngle          api.ParserID = "minecraft:angle"
	ParserHexColor       api.ParserID = "minecraft:hex_color"
	ParserTeamColor      api.ParserID = "minecraft:team_color"
	ParserNBTCompound    api.ParserID = "minecraft:nbt_compound_tag"
	ParserScoreHolder    api.ParserID = "minecraft:score_holder"
	ParserEntityAnchor   api.ParserID = "minecraft:entity_anchor"
	ParserResourceOrTag  api.ParserID = "minecraft:resource_or_tag"
	ParserResourceOrKey  api.ParserID = "minecraft:resource_or_tag_key"
	ParserResourceFull   api.ParserID = "minecraft:resource"
	ParserResourceSelect api.ParserID = "minecraft:resource_selector"
)

// parserAliases pairs names that denote the same parser across versions or
// between api and the 26.3 registry.
var parserAliases = map[api.ParserID]api.ParserID{
	api.ParserColor: ParserTeamColor,
	"minecraft:nbt": ParserNBTCompound,
}

// ParserTable maps parser identifiers to network IDs for one protocol version.
type ParserTable map[api.ParserID]int32

func NewParserTable(order []api.ParserID) ParserTable {
	t := make(ParserTable, len(order)+len(parserAliases))
	for i, id := range order {
		t[id] = int32(i)
	}
	for alias, canon := range parserAliases {
		if id, ok := t[canon]; ok {
			if _, dup := t[alias]; !dup {
				t[alias] = id
			}
		} else if id, ok := t[alias]; ok {
			t[canon] = id
		}
	}
	return t
}

var Table777 = NewParserTable(Parsers777)

func (t ParserTable) Lookup(p api.ParserID) (int32, bool) {
	id, ok := t[p]
	return id, ok
}

func canonicalParser(p api.ParserID) api.ParserID {
	if c, ok := parserAliases[p]; ok {
		return c
	}
	return p
}

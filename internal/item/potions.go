package item

// PotionEffect is one base effect of a potion type.
type PotionEffect struct {
	Effect    string
	Duration  int32
	Amplifier int32
}

// potionEffects lists the base effects of each minecraft:potion entry.
var potionEffects = map[string][]PotionEffect{
	"minecraft:water":                {},
	"minecraft:mundane":              {},
	"minecraft:thick":                {},
	"minecraft:awkward":              {},
	"minecraft:night_vision":         {{"minecraft:night_vision", 3600, 0}},
	"minecraft:long_night_vision":    {{"minecraft:night_vision", 9600, 0}},
	"minecraft:invisibility":         {{"minecraft:invisibility", 3600, 0}},
	"minecraft:long_invisibility":    {{"minecraft:invisibility", 9600, 0}},
	"minecraft:leaping":              {{"minecraft:jump_boost", 3600, 0}},
	"minecraft:long_leaping":         {{"minecraft:jump_boost", 9600, 0}},
	"minecraft:strong_leaping":       {{"minecraft:jump_boost", 1800, 1}},
	"minecraft:fire_resistance":      {{"minecraft:fire_resistance", 3600, 0}},
	"minecraft:long_fire_resistance": {{"minecraft:fire_resistance", 9600, 0}},
	"minecraft:swiftness":            {{"minecraft:speed", 3600, 0}},
	"minecraft:long_swiftness":       {{"minecraft:speed", 9600, 0}},
	"minecraft:strong_swiftness":     {{"minecraft:speed", 1800, 1}},
	"minecraft:slowness":             {{"minecraft:slowness", 1800, 0}},
	"minecraft:long_slowness":        {{"minecraft:slowness", 4800, 0}},
	"minecraft:strong_slowness":      {{"minecraft:slowness", 400, 3}},
	"minecraft:turtle_master":        {{"minecraft:slowness", 400, 3}, {"minecraft:resistance", 400, 2}},
	"minecraft:long_turtle_master":   {{"minecraft:slowness", 800, 3}, {"minecraft:resistance", 800, 2}},
	"minecraft:strong_turtle_master": {{"minecraft:slowness", 400, 5}, {"minecraft:resistance", 400, 3}},
	"minecraft:water_breathing":      {{"minecraft:water_breathing", 3600, 0}},
	"minecraft:long_water_breathing": {{"minecraft:water_breathing", 9600, 0}},
	"minecraft:healing":              {{"minecraft:instant_health", 1, 0}},
	"minecraft:strong_healing":       {{"minecraft:instant_health", 1, 1}},
	"minecraft:harming":              {{"minecraft:instant_damage", 1, 0}},
	"minecraft:strong_harming":       {{"minecraft:instant_damage", 1, 1}},
	"minecraft:poison":               {{"minecraft:poison", 900, 0}},
	"minecraft:long_poison":          {{"minecraft:poison", 1800, 0}},
	"minecraft:strong_poison":        {{"minecraft:poison", 432, 1}},
	"minecraft:regeneration":         {{"minecraft:regeneration", 900, 0}},
	"minecraft:long_regeneration":    {{"minecraft:regeneration", 1800, 0}},
	"minecraft:strong_regeneration":  {{"minecraft:regeneration", 450, 1}},
	"minecraft:strength":             {{"minecraft:strength", 3600, 0}},
	"minecraft:long_strength":        {{"minecraft:strength", 9600, 0}},
	"minecraft:strong_strength":      {{"minecraft:strength", 1800, 1}},
	"minecraft:weakness":             {{"minecraft:weakness", 1800, 0}},
	"minecraft:long_weakness":        {{"minecraft:weakness", 4800, 0}},
	"minecraft:luck":                 {{"minecraft:luck", 6000, 0}},
	"minecraft:slow_falling":         {{"minecraft:slow_falling", 1800, 0}},
	"minecraft:long_slow_falling":    {{"minecraft:slow_falling", 4800, 0}},
	"minecraft:wind_charged":         {{"minecraft:wind_charged", 3600, 0}},
	"minecraft:weaving":              {{"minecraft:weaving", 3600, 0}},
	"minecraft:oozing":               {{"minecraft:oozing", 3600, 0}},
	"minecraft:infested":             {{"minecraft:infested", 3600, 0}},
}

// PotionEffects returns the base effects of a potion by registry id.
func PotionEffects(id int32) []PotionEffect {
	return potionEffects[regName("minecraft:potion", id)]
}

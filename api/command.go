package api

// NodeKind distinguishes literal and argument nodes in a command tree.
type NodeKind uint8

const (
	NodeLiteral NodeKind = iota
	NodeArgument
)

// ParserID is the brigadier/minecraft argument parser identifier sent to the
// client, e.g. "brigadier:integer", "minecraft:entity".
type ParserID string

const (
	ParserBool        ParserID = "brigadier:bool"
	ParserFloat       ParserID = "brigadier:float"
	ParserDouble      ParserID = "brigadier:double"
	ParserInteger     ParserID = "brigadier:integer"
	ParserLong        ParserID = "brigadier:long"
	ParserString      ParserID = "brigadier:string"
	ParserEntity      ParserID = "minecraft:entity"
	ParserGameProfile ParserID = "minecraft:game_profile"
	ParserBlockPos    ParserID = "minecraft:block_pos"
	ParserColumnPos   ParserID = "minecraft:column_pos"
	ParserVec3        ParserID = "minecraft:vec3"
	ParserVec2        ParserID = "minecraft:vec2"
	ParserBlockState  ParserID = "minecraft:block_state"
	ParserItemStack   ParserID = "minecraft:item_stack"
	ParserComponent   ParserID = "minecraft:component"
	ParserMessage     ParserID = "minecraft:message"
	ParserColor       ParserID = "minecraft:color"
	ParserUUID        ParserID = "minecraft:uuid"
	ParserResource    ParserID = "minecraft:resource_location"
	ParserDimension   ParserID = "minecraft:dimension"
	ParserGamemode    ParserID = "minecraft:gamemode"
	ParserTime        ParserID = "minecraft:time"
	ParserRotation    ParserID = "minecraft:rotation"
	ParserResourceKey ParserID = "minecraft:resource_key"
)

type StringMode uint8

const (
	SingleWord StringMode = iota
	QuotablePhrase
	GreedyPhrase
)

// ArgumentType describes how an argument is parsed and how the client renders
// it. Min/Max apply to numeric parsers when HasMin/HasMax are set.
type ArgumentType struct {
	Parser      ParserID
	Min, Max    float64
	HasMin      bool
	HasMax      bool
	StringMode  StringMode
	Single      bool // entity / game_profile: at most one target
	PlayersOnly bool
	Registry    Key // resource_key and similar registry-bound parsers
}

func BoolArg() ArgumentType { return ArgumentType{Parser: ParserBool} }
func IntArg() ArgumentType  { return ArgumentType{Parser: ParserInteger} }
func IntRange(min, max int) ArgumentType {
	return ArgumentType{Parser: ParserInteger, Min: float64(min), Max: float64(max), HasMin: true, HasMax: true}
}
func LongArg() ArgumentType   { return ArgumentType{Parser: ParserLong} }
func DoubleArg() ArgumentType { return ArgumentType{Parser: ParserDouble} }
func FloatArg() ArgumentType  { return ArgumentType{Parser: ParserFloat} }
func WordArg() ArgumentType   { return ArgumentType{Parser: ParserString, StringMode: SingleWord} }
func StringArg() ArgumentType { return ArgumentType{Parser: ParserString, StringMode: QuotablePhrase} }
func GreedyArg() ArgumentType { return ArgumentType{Parser: ParserString, StringMode: GreedyPhrase} }
func PlayerArg() ArgumentType {
	return ArgumentType{Parser: ParserEntity, Single: true, PlayersOnly: true}
}
func PlayersArg() ArgumentType    { return ArgumentType{Parser: ParserEntity, PlayersOnly: true} }
func EntitiesArg() ArgumentType   { return ArgumentType{Parser: ParserEntity} }
func BlockPosArg() ArgumentType   { return ArgumentType{Parser: ParserBlockPos} }
func Vec3Arg() ArgumentType       { return ArgumentType{Parser: ParserVec3} }
func BlockStateArg() ArgumentType { return ArgumentType{Parser: ParserBlockState} }
func ItemArg() ArgumentType       { return ArgumentType{Parser: ParserItemStack} }
func MessageArg() ArgumentType    { return ArgumentType{Parser: ParserMessage} }
func ResourceArg() ArgumentType   { return ArgumentType{Parser: ParserResource} }
func GameModeArg() ArgumentType   { return ArgumentType{Parser: ParserGamemode} }

// CommandNode is a brigadier-style tree node built with Literal/Argument.
// Nodes are plain data and become immutable once registered.
type CommandNode struct {
	Kind        NodeKind
	Name        string
	Type        ArgumentType // NodeArgument only
	Permission  string
	Requirement func(CommandSender) bool
	Executor    CommandExecutor
	Suggest     SuggestionProvider
	Children    []*CommandNode
	Redirect    *CommandNode
	Description string
}

type CommandExecutor func(ctx CommandContext) (int, error)

// SuggestionProvider returns suggestions for the argument being typed.
// Remaining is the text of the current argument up to the cursor.
type SuggestionProvider func(ctx CommandContext, remaining string) []Suggestion

type Suggestion struct {
	Text    string
	Tooltip *Component
}

func Literal(name string) *CommandNode { return &CommandNode{Kind: NodeLiteral, Name: name} }

func Argument(name string, t ArgumentType) *CommandNode {
	return &CommandNode{Kind: NodeArgument, Name: name, Type: t}
}

func (n *CommandNode) Then(children ...*CommandNode) *CommandNode {
	n.Children = append(n.Children, children...)
	return n
}

func (n *CommandNode) Executes(fn CommandExecutor) *CommandNode { n.Executor = fn; return n }

func (n *CommandNode) Requires(fn func(CommandSender) bool) *CommandNode {
	n.Requirement = fn
	return n
}

func (n *CommandNode) RequiresPermission(perm string) *CommandNode { n.Permission = perm; return n }

func (n *CommandNode) Suggests(fn SuggestionProvider) *CommandNode { n.Suggest = fn; return n }

func (n *CommandNode) RedirectTo(target *CommandNode) *CommandNode { n.Redirect = target; return n }

func (n *CommandNode) Describe(desc string) *CommandNode { n.Description = desc; return n }

// CommandContext exposes the parsed arguments of the executing path. Typed
// getters return the zero value when the argument is absent or of another type.
type CommandContext interface {
	Sender() CommandSender
	Input() string
	Label() string
	Has(name string) bool
	Arg(name string) (any, bool)
	String(name string) string
	Int(name string) int64
	Float(name string) float64
	Bool(name string) bool
	Players(name string) []Player
	Entities(name string) []Entity
	BlockPos(name string) BlockPos
	Vec3(name string) Vec3
	BlockState(name string) BlockState
	Item(name string) ItemStack
	Component(name string) Component
}

type CommandInfo struct {
	Name        string
	Aliases     []string
	Owner       string // plugin id, "" for built-in
	Description string
	Usage       string
	Permission  string
}

type CommandRegistry interface {
	// Register adds root under its name, "owner:name" and every alias.
	// Existing commands of other owners keep the plain name.
	Register(owner string, root *CommandNode, aliases ...string) error
	Unregister(owner, name string) bool
	UnregisterAll(owner string)
	Dispatch(sender CommandSender, line string) (int, error)
	Complete(sender CommandSender, line string) []Suggestion
	Commands() []CommandInfo
	Lookup(name string) (CommandInfo, bool)
	// Resend rebuilds and sends the command tree to a player after permission changes.
	Resend(p Player)
}

// CommandError is returned by executors to show a red message to the sender.
type CommandError struct{ Message Component }

func (e *CommandError) Error() string { return e.Message.Plain() }

func Errorf(msg string) error { return &CommandError{Message: Text(msg)} }

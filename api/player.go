package api

import (
	"net/netip"
	"time"
)

type Player interface {
	LivingEntity
	CommandSender
	Audience

	Profile() GameProfile
	DisplayName() Component
	SetDisplayName(Component)
	PlayerListName() Component
	SetPlayerListName(Component)
	Locale() string
	Ping() time.Duration
	Address() netip.AddrPort
	ProtocolVersion() int32
	ClientBrand() string
	IsOnline() bool

	GameMode() GameMode
	SetGameMode(GameMode)
	Flying() bool
	SetFlying(bool)
	AllowFlight() bool
	SetAllowFlight(bool)
	Sneaking() bool
	Sprinting() bool
	Food() int
	SetFood(int)
	Saturation() float32
	SetSaturation(float32)
	Exp() float32
	Level() int
	SetExp(level int, progress float32)
	RespawnLocation() (Location, bool)
	SetRespawnLocation(*Location)

	Inventory() PlayerInventory
	// OpenInventory shows inv to the player, replacing any open container.
	OpenInventory(inv Inventory)
	OpenedInventory() (Inventory, bool)
	CloseInventory()

	Kick(reason Component)
	Chat(msg string)
	PerformCommand(line string) bool
	SendBlockChange(pos BlockPos, state BlockState)
	SendPluginMessage(channel Key, data []byte)
	// Transfer sends the client to another server (configuration/play transfer).
	Transfer(host string, port int)

	CanSee(other Player) bool
	HidePlayer(owner string, other Player)
	ShowPlayer(owner string, other Player)
}

type GameProfile struct {
	UUID       UUID
	Name       string
	Properties []ProfileProperty
}

type ProfileProperty struct {
	Name      string
	Value     string
	Signature string
}

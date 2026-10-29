package api

// CommandSender is anything that can run commands and receive messages:
// players, the console, command blocks, remote consoles.
type CommandSender interface {
	Permissible
	Name() string
	SendMessage(msg Component)
}

type Audience interface {
	SendMessage(msg Component)
	SendActionBar(msg Component)
	ShowTitle(title Title)
	ClearTitle()
	PlaySound(sound Sound)
}

type Title struct {
	Title    Component
	Subtitle Component
	FadeIn   Ticks
	Stay     Ticks
	FadeOut  Ticks
}

type Sound struct {
	Name     Key
	Category string // "master", "music", "record", "weather", "block", "hostile", "neutral", "player", "ambient", "voice"
	Volume   float32
	Pitch    float32
	Seed     int64
	At       *Vec3 // nil plays at the listener
}

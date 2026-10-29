// Test plugin exercising event mutation, tasks, panics and crashes.
package main

import (
	"os"
	"strings"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/kremenmc"
)

var ticks int

func init() {
	kremenmc.Listen(api.Normal, func(e *kremenmc.PlayerChatEvent) {
		if strings.Contains(e.Message, "block") {
			e.SetCancelled(true)
			return
		}
		e.SetMessage(strings.ToUpper(e.Message))
	})
	kremenmc.Listen(api.Monitor, func(e *kremenmc.PlayerChatEvent) {
		e.SetMessage("monitor must not win")
	})
	kremenmc.ListenIgnoreCancelled(api.High, func(e *kremenmc.PlayerChatEvent) {
		e.SetMessage(e.Message + "!")
	})
	kremenmc.Listen(api.Normal, func(e *kremenmc.PlayerMoveEvent) {
		if e.To.Pos.Y < 0 {
			to := e.To
			to.Pos.Y = 64
			e.SetTo(to)
		}
	})
	kremenmc.Listen(api.Normal, func(e *kremenmc.PlayerJoinEvent) {
		if e.Player.Name == "panic" {
			panic("boom")
		}
		e.SetMessage(nil)
	})
	kremenmc.Command("spin", func(*kremenmc.CommandContext) int {
		for {
		}
	})
	kremenmc.Command("die", func(*kremenmc.CommandContext) int {
		os.Exit(3)
		return 0
	})
	kremenmc.OnEnable(func() error {
		kremenmc.RunTimer(1, 1, func() {
			ticks++
			kremenmc.KV.Set("ticks", []byte{byte(ticks)})
		})
		return nil
	})
}

func main() {}

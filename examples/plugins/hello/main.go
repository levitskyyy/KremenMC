// Command hello is an example kremenmc WASM plugin: it greets players on join and
// registers /hello [name].
package main

import (
	"strconv"

	"github.com/levitskyyy/KremenMC/api"
	"github.com/levitskyyy/KremenMC/sdk/go/kremenmc"
)

func init() {
	kremenmc.Listen(api.Normal, func(e *kremenmc.PlayerJoinEvent) {
		key := "joins/" + e.Player.UUID.String()
		n := 0
		if v, ok := kremenmc.KV.Get(key); ok {
			n, _ = strconv.Atoi(string(v))
		}
		n++
		kremenmc.KV.Set(key, []byte(strconv.Itoa(n)))

		msg := "Hello, " + e.Player.Name + "!"
		if n > 1 {
			msg = "Welcome back, " + e.Player.Name + " (visit #" + strconv.Itoa(n) + ")"
		}
		e.Player.SendMessage(api.Colored(msg, "gold"))
	})

	kremenmc.RegisterCommand(kremenmc.Literal("hello").
		Describe("Say hello").
		Executes(func(c *kremenmc.CommandContext) int {
			c.Reply(api.Text("Hello, " + c.SenderName + "!"))
			return 1
		}).
		Then(kremenmc.Argument("name", api.WordArg()).
			Suggest("world", "server").
			Executes(func(c *kremenmc.CommandContext) int {
				c.Reply(api.Text("Hello, " + c.String("name") + "!"))
				return 1
			})))

	kremenmc.OnEnable(func() error {
		kremenmc.Infof("hello plugin enabled")
		return nil
	})
}

func main() {}

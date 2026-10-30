# KremenMC

KremenMC is a Minecraft: Java Edition server written from scratch in Go.

The goal is a dependable server for real communities: stable under load, light on CPU and memory, and fully multithreaded. It should run well on large machines and still be usable on a weak single-core VPS with a small amount of RAM.

KremenMC is under active development and is not ready for production use yet.

## Goals

- **Vanilla behaviour.** Gameplay and world generation are meant to match the vanilla server of the supported version. This is checked against the official server in tests, never at runtime.
- **Low resource usage.** No JVM. A single static binary, small memory footprint, fast startup.
- **Multithreading.** Worlds are split into regions that tick in parallel. Chunk generation, loading and saving run on their own worker pool. World generation is deterministic regardless of thread count.
- **Scales down.** The same design runs on one core without extra overhead.
- **Plugins.** Native WebAssembly plugins, plus an optional bridge for existing Bukkit and Paper plugins.

## Supported versions

| | |
| --- | --- |
| Primary target | Java Edition 26.3 (protocol 777) |
| Older clients | 1.20.5 and newer, through the built-in translation layer |

## Current state

Working:
- Login, online mode and encryption
- Chunk streaming
- Survival basics: breaking and placing, inventory, combat, damage and death
- Block placement states and neighbour updates (fences, walls, stairs, doors, beds, double chests)
- Water and lava flow, buckets
- Mob AI with pathfinding, mob variants, spawn equipment, breeding and taming
- Explosions and TNT
- Sounds for mobs, players and blocks
- The Nether and the End, with portals
- Commands
- World saving in the Anvil format
- Paper and Spigot plugins through the Java bridge, including plugins built on PacketEvents

World generation follows vanilla. Terrain, biomes, caves, ores, most vegetation, villages, mineshafts, dungeons and several smaller structures are in. The remaining structures and features are being added one by one, each only after it matches vanilla.

Missing or incomplete: redstone, fire spread, villager trading, several structures, and parts of the plugin API.

## Downloads

Prebuilt binaries for Linux and macOS (amd64 and arm64) are attached to each [release](https://github.com/levitskyyy/KremenMC/releases).

## Building

Requires Go 1.27 or newer.

```
git clone https://github.com/levitskyyy/KremenMC.git
cd KremenMC
go build -o kremenmc ./cmd/kremenmc
```

The optional Java plugin bridge needs JDK 25 and is built separately:

```
go run ./tools/bridgebuild
```

## Running

```
./kremenmc -dir server
```

The server directory holds `server.properties`, `kremenmc.toml`, worlds and plugins. Existing `server.properties` files from vanilla or Paper are read as is.

| Flag | Description |
| --- | --- |
| `-dir` | server directory, default `.` |
| `-log-level` | `debug`, `info`, `warn` or `error` |
| `-pprof` | serve profiling data on the given address |

## Project layout

| Path | Contents |
| --- | --- |
| `cmd/kremenmc` | entry point |
| `api` | public plugin API |
| `internal/net` | connections, login, encryption, compression |
| `internal/proto` | protocol types and packets |
| `internal/translate` | translation for older protocol versions |
| `internal/world` | chunks, lighting, storage, world generation |
| `internal/entity` | entities and physics |
| `internal/server` | game logic, worlds, players, tick loop |
| `internal/plugin` | WebAssembly plugin host |
| `internal/bridge`, `bridge/java` | Java plugin bridge |
| `sdk/go` | SDK for writing plugins in Go |
| `tools` | development tools, not used at runtime |

## Contributing

Issues and pull requests are welcome. Run `go vet ./...` and the tests of the packages you changed before opening a pull request.

## License

KremenMC is licensed under the [GNU General Public License v3.0](LICENSE).

The plugin API (`api`), the plugin SDK (`sdk`) and the examples (`examples`) are licensed under the MIT License, so plugins can use any license.

KremenMC is not an official Minecraft product and is not affiliated with Mojang or Microsoft.

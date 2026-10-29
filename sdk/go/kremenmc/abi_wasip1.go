//go:build wasip1

package kremenmc

import "github.com/levitskyyy/KremenMC/sdk/go/wire"

//go:wasmimport kremenmc log
func hostLog(level int32, ptr, n uint32)

//go:wasmimport kremenmc result
func hostResult(ptr uint32)

//go:wasmimport kremenmc send_message
func hostSendMessage(uuid, ptr, n uint32) int32

//go:wasmimport kremenmc broadcast
func hostBroadcast(ptr, n uint32)

//go:wasmimport kremenmc players
func hostPlayers() int32

//go:wasmimport kremenmc player
func hostPlayer(uuid uint32) int32

//go:wasmimport kremenmc player_by_name
func hostPlayerByName(ptr, n uint32) int32

//go:wasmimport kremenmc teleport
func hostTeleport(uuid, ptr, n uint32) int32

//go:wasmimport kremenmc kick
func hostKick(uuid, ptr, n uint32) int32

//go:wasmimport kremenmc has_permission
func hostHasPermission(uuid, ptr, n uint32) int32

//go:wasmimport kremenmc get_block
func hostGetBlock(wptr, wn uint32, x, y, z int32) int32

//go:wasmimport kremenmc set_block
func hostSetBlock(wptr, wn uint32, x, y, z int32, sptr, sn uint32) int32

//go:wasmimport kremenmc register_command
func hostRegisterCommand(ptr, n uint32) int32

//go:wasmimport kremenmc subscribe
func hostSubscribe(kind, priority, ignoreCancelled uint32) int32

//go:wasmimport kremenmc schedule
func hostSchedule(id uint32, delay, period int64, async uint32) int32

//go:wasmimport kremenmc cancel_task
func hostCancelTask(id uint32)

//go:wasmimport kremenmc dispatch_command
func hostDispatchCommand(uuid, ptr, n uint32) int32

//go:wasmimport kremenmc kv_get
func hostKVGet(kptr, kn uint32) int32

//go:wasmimport kremenmc kv_set
func hostKVSet(kptr, kn, vptr, vn uint32) int32

//go:wasmimport kremenmc kv_delete
func hostKVDelete(kptr, kn uint32) int32

//go:wasmimport kremenmc worlds
func hostWorlds() int32

//go:wasmexport kremenmc_alloc
func kremenmcAlloc(size uint32) uint32 { return alloc(size) }

//go:wasmexport kremenmc_enable
func kremenmcEnable() int32 { return enable() }

//go:wasmexport kremenmc_disable
func kremenmcDisable() { disable() }

//go:wasmexport kremenmc_on_event
func kremenmcOnEvent(kind, ptr, n uint32) uint64 { return onEvent(kind, input(ptr, n)) }

//go:wasmexport kremenmc_on_command
func kremenmcOnCommand(handler, ptr, n uint32) int32 { return onCommand(handler, input(ptr, n)) }

//go:wasmexport kremenmc_on_task
func kremenmcOnTask(id uint32) { onTask(id) }

//go:wasmexport kremenmc_abi_version
func kremenmcABIVersion() uint32 { return wire.ABIVersion }

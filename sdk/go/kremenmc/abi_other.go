//go:build !wasip1

package kremenmc

// Host stubs so plugins and the SDK compile and unit-test on the host OS.
// Every call behaves as if the server has nothing to offer.

func hostLog(int32, uint32, uint32)                                          {}
func hostResult(uint32)                                                      {}
func hostSendMessage(uint32, uint32, uint32) int32                           { return -1 }
func hostBroadcast(uint32, uint32)                                           {}
func hostPlayers() int32                                                     { return -1 }
func hostPlayer(uint32) int32                                                { return -1 }
func hostPlayerByName(uint32, uint32) int32                                  { return -1 }
func hostTeleport(uint32, uint32, uint32) int32                              { return -1 }
func hostKick(uint32, uint32, uint32) int32                                  { return -1 }
func hostHasPermission(uint32, uint32, uint32) int32                         { return 0 }
func hostGetBlock(uint32, uint32, int32, int32, int32) int32                 { return -1 }
func hostSetBlock(uint32, uint32, int32, int32, int32, uint32, uint32) int32 { return -1 }
func hostRegisterCommand(uint32, uint32) int32                               { return 1 }
func hostSubscribe(uint32, uint32, uint32) int32                             { return 1 }
func hostSchedule(uint32, int64, int64, uint32) int32                        { return 1 }
func hostCancelTask(uint32)                                                  {}
func hostDispatchCommand(uint32, uint32, uint32) int32                       { return -1 }
func hostKVGet(uint32, uint32) int32                                         { return -1 }
func hostKVSet(uint32, uint32, uint32, uint32) int32                         { return -1 }
func hostKVDelete(uint32, uint32) int32                                      { return -1 }
func hostWorlds() int32                                                      { return -1 }

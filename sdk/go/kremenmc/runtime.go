package kremenmc

import (
	"fmt"
	"runtime/debug"
	"unsafe"

	"github.com/levitskyyy/KremenMC/sdk/go/wire"
)

var (
	inbuf    []byte
	respBuf  wire.Writer
	enabled  bool
	enableFn []func() error
	disableF []func()
)

// OnEnable registers fn to run when the server enables the plugin. Returning
// an error aborts enabling.
func OnEnable(fn func() error) { enableFn = append(enableFn, fn) }

// OnDisable registers fn to run when the plugin is disabled or reloaded.
func OnDisable(fn func()) { disableF = append(disableF, fn) }

func alloc(size uint32) uint32 {
	if cap(inbuf) < int(size) || inbuf == nil {
		inbuf = make([]byte, max(int(size), 4096))
	}
	inbuf = inbuf[:size]
	return addr(inbuf[:cap(inbuf)])
}

// input returns the buffer the host just filled via kremenmc_alloc.
func input(ptr, n uint32) []byte {
	if n == 0 || int(n) > cap(inbuf) || ptr != addr(inbuf[:cap(inbuf)]) {
		return nil
	}
	return inbuf[:n]
}

func addr(b []byte) uint32 {
	if len(b) == 0 {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(&b[0])))
}

func strAddr(s string) (uint32, uint32) {
	if s == "" {
		return 0, 0
	}
	return uint32(uintptr(unsafe.Pointer(unsafe.StringData(s)))), uint32(len(s))
}

// Registrations made before the first enable (package init) are replayed on
// every enable; ones made during or after enable are dropped on disable,
// matching how the host forgets them.
var staticListeners, staticCommands = -1, -1

func enable() (status int32) {
	defer func() {
		if v := recover(); v != nil {
			Errorf("enable panicked: %v\n%s", v, debug.Stack())
			status = 1
		}
	}()
	if staticListeners < 0 {
		staticListeners, staticCommands = len(listeners), len(commands)
	}
	enabled = true
	for i := range listeners {
		listeners[i].subscribe(i)
	}
	for _, c := range commands {
		if err := registerCommand(c.node, c.aliases); err != nil {
			Errorf("%v", err)
		}
	}
	for _, fn := range enableFn {
		if err := fn(); err != nil {
			Errorf("enable: %v", err)
			return 1
		}
	}
	return 0
}

func disable() {
	defer func() {
		if v := recover(); v != nil {
			Errorf("disable panicked: %v", v)
		}
	}()
	for _, fn := range disableF {
		fn()
	}
	enabled = false
	listeners = listeners[:staticListeners]
	commands = commands[:staticCommands]
	handlers = handlers[:1]
	clear(tasks)
}

// guard runs fn and converts a panic into a logged error so one faulty
// handler does not take the whole plugin down.
func guard(what string, fn func()) (ok bool) {
	defer func() {
		if v := recover(); v != nil {
			Errorf("%s panicked: %v\n%s", what, v, debug.Stack())
			ok = false
		}
	}()
	fn()
	return true
}

// fetch copies a staged host result of length n.
func fetch(n int32) ([]byte, bool) {
	if n < 0 {
		return nil, false
	}
	buf := make([]byte, n)
	if n > 0 {
		hostResult(addr(buf))
	}
	return buf, true
}

func logAt(level wire.LogLevel, msg string) {
	p, n := strAddr(msg)
	hostLog(int32(level), p, n)
}

func Debugf(format string, args ...any) { logAt(wire.LogDebug, fmt.Sprintf(format, args...)) }
func Infof(format string, args ...any)  { logAt(wire.LogInfo, fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { logAt(wire.LogWarn, fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { logAt(wire.LogError, fmt.Sprintf(format, args...)) }

package metrics

import (
	"encoding/binary"

	"golang.org/x/sys/unix"

	"github.com/levitskyyy/KremenMC/api"
)

// platform on darwin reports the load average via sysctl. Per-core CPU
// (host_processor_info) and resident size (task_info) are Mach calls that
// need cgo, so CPUCores stays nil and RSS is estimated from the runtime.
type platform struct{}

func (platform) sample(st *api.ServerStats) {
	st.Load, st.HasLoad = darwinLoadAvg()
}

// darwinLoadAvg decodes struct loadavg { fixpt_t ldavg[3]; long fscale; }.
func darwinLoadAvg() ([3]float64, bool) {
	var l [3]float64
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return l, false
	}
	return decodeLoadavg(b)
}

func decodeLoadavg(b []byte) ([3]float64, bool) {
	var l [3]float64
	if len(b) < 24 {
		return l, false
	}
	scale := float64(binary.LittleEndian.Uint64(b[16:24]))
	if scale <= 0 {
		return l, false
	}
	for i := range 3 {
		l[i] = float64(binary.LittleEndian.Uint32(b[i*4:])) / scale
	}
	return l, true
}

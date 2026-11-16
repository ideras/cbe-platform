//go:build !linux

package loadtest

// Host reads what the machine is doing: on Linux the load, the processes and the control groups (host_linux.go); on any other system, only what the caller's
// Extra says. The students of a load test that runs on a laptop do not need the laptop's own readings.
type Host struct {
	Pids   map[string]int
	Names  map[string]string
	Groups map[string]string
	Extra  func() map[string]float64
}

// Sample reads what there is to read.
func (h *Host) Sample() map[string]float64 {
	if h.Extra != nil {
		return h.Extra()
	}
	return map[string]float64{}
}

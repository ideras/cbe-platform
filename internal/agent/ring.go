//go:build linux

package agent

// ring keeps the last bytes of a terminal's output, for a client that attaches later.
type ring struct {
	buf   []byte
	start int
	size  int
}

func newRing(capacity int) *ring { return &ring{buf: make([]byte, capacity)} }

func (r *ring) write(data []byte) {
	n := len(r.buf)
	if len(data) >= n {
		copy(r.buf, data[len(data)-n:])
		r.start, r.size = 0, n
		return
	}
	end := (r.start + r.size) % n
	copied := copy(r.buf[end:], data)
	copy(r.buf, data[copied:])
	r.size += len(data)
	if r.size > n {
		r.start = (r.start + r.size - n) % n
		r.size = n
	}
}

// snapshot returns what the ring holds, oldest first, as a new slice.
func (r *ring) snapshot() []byte {
	out := make([]byte, r.size)
	n := copy(out, r.buf[r.start:min(r.start+r.size, len(r.buf))])
	copy(out[n:], r.buf[:r.size-n])
	return out
}

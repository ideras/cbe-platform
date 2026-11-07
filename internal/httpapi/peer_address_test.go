package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestPeerAddressTrustsForwardingOnlyFromThisComputer(t *testing.T) {
	for name, tc := range map[string]struct {
		remote    string
		forwarded []string
		want      string
	}{
		"direct":                  {"192.0.2.20:4567", nil, "192.0.2.20"},
		"claim from a stranger":   {"192.0.2.20:4567", []string{"203.0.113.9"}, "192.0.2.20"},
		"proxy on this computer":  {"127.0.0.1:5555", []string{"203.0.113.9"}, "203.0.113.9"},
		"proxy over IPv6":         {"[::1]:5555", []string{"2001:db8::7"}, "2001:db8::7"},
		"last entry is the proxy": {"127.0.0.1:5555", []string{"198.51.100.1, 203.0.113.9"}, "203.0.113.9"},
		"last header wins":        {"127.0.0.1:5555", []string{"198.51.100.1", "203.0.113.9"}, "203.0.113.9"},
		"zone is dropped":         {"127.0.0.1:5555", []string{"fe80::1%eth0"}, "fe80::1"},
		"garbage is ignored":      {"127.0.0.1:5555", []string{"not-an-address"}, "127.0.0.1"},
		"no header":               {"127.0.0.1:5555", nil, "127.0.0.1"},
		"no port":                 {"192.0.2.20", nil, "192.0.2.20"},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/login", nil)
			r.RemoteAddr = tc.remote
			for _, value := range tc.forwarded {
				r.Header.Add("X-Forwarded-For", value)
			}
			if got := peerAddress(r); got != tc.want {
				t.Fatalf("peerAddress = %q, want %q", got, tc.want)
			}
		})
	}
}

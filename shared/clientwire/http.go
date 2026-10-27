// Package clientwire contains credential-neutral desktop transport and wire contracts.
package clientwire

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Final struct {
	Status      string    `json:"status"`
	SubmittedAt time.Time `json:"submitted_at"`
	VersionID   int64     `json:"final_version_id"`
	Sequence    int64     `json:"version_seq"`
}

func ValidateServer(value string) (string, error) {
	value = strings.TrimSpace(value)
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery || u.RawPath != "" || (u.Path != "" && u.Path != "/") || strings.ContainsFunc(value, unicode.IsControl) {
		return "", errors.New("Use an HTTP server address with a host, without credentials, path, query or fragment.")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errors.New("Server port must be between 1 and 65535.")
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", errors.New("Server port is required after a colon.")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: timeout, IdleConnTimeout: 30 * time.Second, MaxIdleConnsPerHost: 2}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

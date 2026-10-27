package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// AdminAuth uses a proctor-only credential, never student session identity.
func AdminAuth(token string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, provided, hasToken := strings.Cut(r.Header.Get("Authorization"), " ")
		actual := sha256.Sum256([]byte(provided))
		if len(r.Header.Values("Authorization")) != 1 || token == "" || !hasToken || !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="proctor"`)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

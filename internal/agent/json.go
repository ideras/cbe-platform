//go:build linux

package agent

import (
	"bytes"
	"encoding/json"
)

// jsonUnmarshalLenient reads the first JSON value of payload into a struct, ignoring fields it does not know and anything after the
// value. It is only used to find the ID and type of a request that failed to decode, so the reply can name it.
func jsonUnmarshalLenient(payload []byte, into any) error {
	return json.NewDecoder(bytes.NewReader(payload)).Decode(into)
}

package examimage

import (
	"encoding/base64"
	"fmt"
	"html"
	"regexp"
)

var (
	// opening finds every <img the browser could read as an image, whatever its case.
	opening = regexp.MustCompile(`(?i)<img`)
	// tag is exactly what HTML writes.
	tag = regexp.MustCompile(`<img src="data:(image/(?:png|jpeg|gif|svg\+xml));base64,([A-Za-z0-9+/]+={0,2})" alt="([^"<>]*)">`)
)

// CheckFragment checks every picture in an exam's HTML fragment: each <img must be exactly what HTML writes, and its payload
// must be a picture Validate accepts, of the kind it declares. It returns how many bytes of picture files the fragment holds,
// for the caller to count against MaxTotalBytes. A fragment without pictures costs one scan.
func CheckFragment(fragment string) (int, error) {
	opened := len(opening.FindAllStringIndex(fragment, -1))
	if opened == 0 {
		return 0, nil
	}
	matches := tag.FindAllStringSubmatch(fragment, -1)
	if len(matches) != opened {
		return 0, fmt.Errorf("it has an <img> that is not an embedded %s picture written by the exam compiler (only src=\"data:…\" and alt are allowed)", Formats)
	}
	total := 0
	for i, match := range matches {
		mime, payload, alt := match[1], match[2], html.UnescapeString(match[3])
		if len(payload) > base64.StdEncoding.EncodedLen(MaxImageBytes) {
			return 0, fmt.Errorf("image %d: the file is larger than %d KiB", i+1, MaxImageBytes>>10)
		}
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return 0, fmt.Errorf("image %d: the data is not valid base64", i+1)
		}
		kind, err := Validate(data)
		if err != nil {
			return 0, fmt.Errorf("image %d: %v", i+1, err)
		}
		if kind != mime {
			return 0, fmt.Errorf("image %d: it is declared as %s but its content is %s", i+1, mime, kind)
		}
		if err := CheckAlt(alt); err != nil {
			return 0, fmt.Errorf("image %d: %v", i+1, err)
		}
		total += len(data)
	}
	return total, nil
}

package service

import (
	"encoding/json"
	"unicode/utf8"

	"exam-platform/internal/domain"
)

// maxRemoteFindings and maxRemoteText keep what the roster carries small: what the student application says it found is its own
// word, so it is bounded, and shown as plain text.
const (
	maxRemoteFindings = 10
	maxRemoteText     = 64
)

// remoteFindings is what a remote_changed report says the student application found. A report that cannot be read says nothing was
// found, and so does one that names no known kind of finding.
func remoteFindings(detail []byte) []domain.RemoteFinding {
	found := []domain.RemoteFinding{}
	var stored struct {
		Client struct {
			Active []domain.RemoteFinding `json:"active"`
		} `json:"client"`
	}
	if json.Unmarshal(detail, &stored) != nil {
		return found
	}
	for _, finding := range stored.Client.Active {
		if (finding.Kind != domain.RemoteKindSession && finding.Kind != domain.RemoteKindTool) || finding.Name == "" {
			continue
		}
		found = append(found, domain.RemoteFinding{Kind: finding.Kind, Name: cutText(finding.Name, maxRemoteText), Detail: cutText(finding.Detail, maxRemoteText)})
		if len(found) == maxRemoteFindings {
			break
		}
	}
	return found
}

// cutText cuts a string to at most n bytes without splitting a character.
func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

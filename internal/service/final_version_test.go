package service

import (
	"testing"

	"exam-platform/internal/domain"
)

func TestValidateFinalVersion(t *testing.T) {
	if err := ValidateFinalVersion("session-a", domain.Version{ID: 1, SessionID: "session-a"}); err != nil {
		t.Fatal(err)
	}
	for _, version := range []domain.Version{{ID: 1, SessionID: "session-b"}, {ID: 0, SessionID: "session-a"}} {
		if err := ValidateFinalVersion("session-a", version); err == nil {
			t.Fatal("invalid final-version ownership accepted")
		}
	}
}

package service

import (
	"fmt"

	"exam-platform/internal/domain"
)

// ValidateFinalVersion must run before a lifecycle transaction assigns its final-version reference.
func ValidateFinalVersion(sessionID string, version domain.Version) error {
	if sessionID == "" || version.ID <= 0 || version.SessionID != sessionID {
		return fmt.Errorf("final version must belong to the session")
	}
	return nil
}

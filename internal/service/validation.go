package service

import (
	"fmt"
	"strings"
)

const maxProblems = 20

// ValidationError lists what is wrong with an administrator's document so an authoring tool can show it. It matches
// ErrInvalidInput. Problems name fields, rows and question IDs but never echo secrets such as the exam PIN.
type ValidationError struct{ Problems []string }

func (e *ValidationError) add(format string, args ...any) {
	if len(e.Problems) == maxProblems {
		e.Problems = append(e.Problems, "more problems were found; fix these first")
		return
	}
	if len(e.Problems) < maxProblems {
		e.Problems = append(e.Problems, fmt.Sprintf(format, args...))
	}
}
func (e *ValidationError) Error() string {
	return ErrInvalidInput.Error() + ": " + strings.Join(e.Problems, "; ")
}
func (e *ValidationError) Is(target error) bool { return target == ErrInvalidInput }

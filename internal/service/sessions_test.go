package service

import (
	"math"
	"testing"
	"time"

	"exam-platform/internal/domain"
)

func TestExamTimingRejectsOverflow(t *testing.T) {
	for _, exam := range []domain.Exam{
		{DurationSeconds: 0, GraceSeconds: 30}, {DurationSeconds: 60, GraceSeconds: 0},
		{DurationSeconds: -1, GraceSeconds: 30}, {DurationSeconds: math.MaxInt64, GraceSeconds: 30},
		{DurationSeconds: math.MaxInt64 / int64(time.Second), GraceSeconds: 1},
		{DurationSeconds: 60, GraceSeconds: math.MaxInt64},
	} {
		if _, _, err := examTiming(exam); err == nil {
			t.Fatalf("invalid timing accepted: %d / %d", exam.DurationSeconds, exam.GraceSeconds)
		}
	}
	duration, grace, err := examTiming(domain.Exam{DurationSeconds: 60, GraceSeconds: 7})
	if err != nil || duration != time.Minute || grace != 7*time.Second {
		t.Fatalf("timing = %s / %s, %v", duration, grace, err)
	}
}

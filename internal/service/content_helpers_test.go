package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"exam-platform/internal/clock"
	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// examContent has one exercise per list of slot IDs, each slot a text answer placed in the exercise's body.
func examContent(exercises ...[]string) domain.ExamContent {
	var content domain.ExamContent
	for i, ids := range exercises {
		exercise := domain.Exercise{ID: fmt.Sprint(i + 1), Title: fmt.Sprintf("Ejercicio %d 🧪", i+1)}
		for _, id := range ids {
			exercise.Body += `<p>¿Qué imprime 世界?</p><div class="slot" data-slot="` + id + `"></div>`
			exercise.Slots = append(exercise.Slots, domain.Slot{ID: id, Kind: domain.TextSlot})
		}
		content.Exercises = append(content.Exercises, exercise)
	}
	return content
}

// setContent replaces the test exam's content and navigation.
func setContent(t *testing.T, f *fixture, navigation domain.Navigation, content domain.ExamContent) {
	t.Helper()
	encoded, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.raw.Exec(`UPDATE exams SET content_json = jsonb(?), navigation = ? WHERE exam_id = ?`, string(encoded), navigation, testExamID); err != nil {
		t.Fatal(err)
	}
}

// testSessions is the session service the tests use. The monitoring settings come from the store: the fixtures store
// testPolicy before they publish the exam, so the exam freezes it like the proctor's Settings page would.
func testSessions(repository service.SessionRepository, clock clock.Clock) *service.Sessions {
	sessions, err := service.NewSessionsWithLimits(repository, clock, service.DefaultAnswerLimits())
	if err != nil {
		panic(err)
	}
	return sessions
}

// testPolicy is the monitoring settings the tests run with: a 20-second focus threshold, as they were written for.
func testPolicy() domain.Policy {
	policy := domain.DefaultPolicy()
	policy.FocusThreshold = 20 * time.Second
	return policy
}

// storeSettings writes the monitoring settings of the server, as the proctor's Settings page does.
func storeSettings(repository service.SessionRepository, clock clock.Clock, policy domain.Policy) {
	err := repository.WithSessionTransaction(context.Background(), func(tx service.SessionTransaction) error {
		return tx.SaveSettings(context.Background(), policy.Normalized(), clock.Now())
	})
	if err != nil {
		panic(err)
	}
}

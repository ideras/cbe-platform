package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
)

// jsonField walks a decoded JSON document and fails the test when a key is missing.
// The spec requires unavailable fields to be present as null, never omitted.
func jsonField(t *testing.T, doc map[string]any, path ...string) any {
	t.Helper()
	node := any(doc)
	for _, key := range path {
		object, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("%v: expected an object at %q, got %T", path, key, node)
		}
		node, ok = object[key]
		if !ok {
			t.Fatalf("%v: missing key %q; the spec requires unavailable fields to be null", path, key)
		}
	}
	return node
}

func jsonNumber(t *testing.T, value any) float64 {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("expected a number, got %T (%v)", value, value)
	}
	return number
}

func TestStudentEvaluationJSONConformsToSpec(t *testing.T) {
	exam := ExamEntry{ID: "exam-1", ClassName: "Computer Science", Title: "Compiler Lab"}
	student := Student{Account: "977183", Name: "Sam & Lee"}
	gradings := WorkspaceGradings{
		CanvasID: "32463",
		Todos: []GradingTodo{
			{ID: "parse", Text: "Parse input", Points: new(5.0)},
			{ID: "format", Text: "Format output"},
		},
	}
	run := Grading{SubmissionID: 42, Confirmed: true, Points: new(6.256), Max: new(14.0), Results: []GradingResult{
		{TodoID: "parse", Known: true, Passed: true, Points: 5, Log: "all cases passed"},
		{TodoID: "format", Known: true, Points: 1.256},
	}}
	raw, err := renderStudentEvaluationJSON(exam, student, gradings, run, map[string]bool{"parse": true})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("output does not parse as JSON: %v\n%s", err, raw)
	}

	// Student: canvas_id and account_number available, gender unavailable but present as null.
	studentField := jsonField(t, doc, "student").(map[string]any)
	if name, _ := studentField["name"].(string); name != "Sam & Lee" {
		t.Fatalf("student.name = %v, want Sam & Lee", name)
	}
	if id := jsonNumber(t, jsonField(t, studentField, "canvas_id")); id != 32463 {
		t.Fatalf("student.canvas_id = %v, want 32463", id)
	}
	if account, _ := jsonField(t, studentField, "account_number").(string); account != "977183" {
		t.Fatalf("student.account_number = %v, want 977183", account)
	}
	if gender := jsonField(t, studentField, "gender"); gender != nil {
		t.Fatalf("student.gender = %v, want null", gender)
	}

	// Exam: the original rubric score is preserved, promotion fields null.
	if course, _ := jsonField(t, doc, "exam", "course").(string); course != "Computer Science" {
		t.Fatalf("exam.course = %v, want Computer Science", course)
	}
	if name, _ := jsonField(t, doc, "exam", "name").(string); name != "Compiler Lab" {
		t.Fatalf("exam.name = %v, want Compiler Lab", name)
	}
	if got := jsonNumber(t, jsonField(t, doc, "exam", "score")); got != 6.256 {
		t.Fatalf("exam.score = %v, want the rubric score 6.256", got)
	}
	if got := jsonNumber(t, jsonField(t, doc, "exam", "total_points")); got != 14 {
		t.Fatalf("exam.total_points = %v, want 14", got)
	}
	if promoted := jsonField(t, doc, "exam", "promoted_score"); promoted != nil {
		t.Fatalf("exam.promoted_score = %v, want null", promoted)
	}
	if reason := jsonField(t, doc, "exam", "promotion_reason"); reason != nil {
		t.Fatalf("exam.promotion_reason = %v, want null", reason)
	}

	// Hierarchy: section > exercise > evaluated_items, with type discriminators.
	sections, ok := jsonField(t, doc, "sections").([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("sections = %v, want exactly one section", doc["sections"])
	}
	section, _ := sections[0].(map[string]any)
	if section["type"] != "section" {
		t.Fatalf("section.type = %v, want section", section["type"])
	}
	if _, hasExercises := section["sections"].([]any); !hasExercises {
		t.Fatal("section.sections is missing; the spec requires the key even when empty")
	}
	exercises, ok := section["exercises"].([]any)
	if !ok || len(exercises) != 1 {
		t.Fatalf("section.exercises = %v, want exactly one exercise", section["exercises"])
	}
	exercise, _ := exercises[0].(map[string]any)
	if exercise["type"] != "exercise" {
		t.Fatalf("exercise.type = %v, want exercise", exercise["type"])
	}
	items, ok := exercise["evaluated_items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("exercise.evaluated_items = %v, want two items", exercise["evaluated_items"])
	}
	first, _ := items[0].(map[string]any)
	if first["name"] != "parse" || first["description"] != "Parse input" {
		t.Fatalf("item[0] name/description = %v/%v", first["name"], first["description"])
	}
	if got := jsonNumber(t, first["score"]); got != 5 {
		t.Fatalf("item[0].score = %v, want 5", got)
	}
	if got := jsonNumber(t, first["total_points"]); got != 5 {
		t.Fatalf("item[0].total_points = %v, want 5", got)
	}
	// comment is REQUIRED on every evaluated item, even without feedback.
	for i, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		comment, ok := item["comment"].(string)
		if !ok || strings.TrimSpace(comment) == "" {
			t.Fatalf("item[%d].comment = %v; the spec requires a non-empty justification", i, item["comment"])
		}
		if _, ok := item["evaluated_items"].([]any); !ok {
			t.Fatalf("item[%d] is missing evaluated_items; the spec requires the key", i)
		}
	}
	if second, _ := items[1].(map[string]any); second["comment"] == "" {
		t.Fatal("item[1].comment is empty")
	}
}

func TestStudentEvaluationJSONNullsForUnavailableFields(t *testing.T) {
	exam := ExamEntry{ID: "exam-1"}
	student := Student{Account: "", Name: "Sam"}
	gradings := WorkspaceGradings{CanvasID: "not-a-number", Todos: nil}
	run := Grading{SubmissionID: 1}
	raw, err := renderStudentEvaluationJSON(exam, student, gradings, run, nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("output does not parse as JSON: %v\n%s", err, raw)
	}
	if canvas := jsonField(t, doc, "student", "canvas_id"); canvas != nil {
		t.Fatalf("student.canvas_id = %v, want null for a non-numeric ID", canvas)
	}
	if account := jsonField(t, doc, "student", "account_number"); account != nil {
		t.Fatalf("student.account_number = %v, want null when unknown", account)
	}
	if name := jsonField(t, doc, "exam", "name"); name != nil {
		t.Fatalf("exam.name = %v, want null when untitled", name)
	}
	if score := jsonNumber(t, jsonField(t, doc, "exam", "score")); score != 0 {
		t.Fatalf("exam.score = %v, want 0", score)
	}
	if sections, ok := doc["sections"].([]any); !ok || len(sections) != 0 {
		t.Fatalf("sections = %v, want an empty array", doc["sections"])
	}
}

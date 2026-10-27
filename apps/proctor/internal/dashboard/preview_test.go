package dashboard

import (
	"net/http"
	"testing"

	"exam-platform/shared/calc"
	"exam-platform/shared/examdoc"
)

// previewReply is the JSON the server's exam detail carries; the test's fake server answers with it.
type previewReply struct {
	ExamID     string          `json:"exam_id"`
	Title      string          `json:"title"`
	Navigation string          `json:"navigation"`
	Tools      *calc.Tools     `json:"tools,omitempty"`
	Content    examdoc.Content `json:"content"`
}

// The exam preview dialog (owner decision 2026-10-23): PreviewExam loads a stored exam as the students see it,
// and the calculator the dialog embeds evaluates through this service with the same engine as the student
// application — configured angle mode wins, the allowed set comes from the exam, and a preview without a
// calculator refuses loudly.
func TestTheExamPreviewLoadsAndItsCalculatorEvaluates(t *testing.T) {
	tools := &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific, calc.ModeProgrammer}, AngleMode: calc.AngleDegrees}}
	detail := previewReply{ExamID: examID, Title: "Prueba", Navigation: "free", Content: examdoc.Content{Exercises: []examdoc.Exercise{{ID: "1", Title: "Ejercicio 1"}}}, Tools: tools}
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/admin/exam/"+examID {
			reply(w, detail)
			return
		}
		if r.URL.Path == "/admin/sessions" {
			reply(w, roster())
			return
		}
		http.NotFound(w, r)
	})
	view, err := s.PreviewExam(examID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Title != "Prueba" || len(view.Content.Exercises) != 1 || view.Calculator == nil || view.Calculator.Angle != "deg" {
		t.Fatalf("preview = %+v", view)
	}
	if allowed := s.CalcAllowedFunctions(); !contains(allowed, "sin") || contains(allowed, "sigmoid") {
		t.Fatalf("allowed = %v", allowed)
	}
	// The configured angle mode wins; sin(90) is exactly 1 in a deg exam.
	if result, err := s.CalcEvaluate("sin(90)", "rad"); err != nil || !result.Ok || result.Display != "1" {
		t.Fatalf("sin(90) = %+v, %v", result, err)
	}
	// The programmer panel: the ans survives between evaluations.
	if result, err := s.CalcEvaluateInt("0xFF + 1", 8, "hex", true); err != nil || !result.Ok || result.Unsigned != "0" || !result.Carry {
		t.Fatalf("0xFF + 1 = %+v, %v", result, err)
	}
	if result, err := s.CalcEvaluateInt("ans", 8, "hex", true); err != nil || !result.Ok || result.Unsigned != "0" {
		t.Fatalf("ans = %+v, %v", result, err)
	}
	if result, err := s.CalcConvertFraction("10.625", 8, "dec", true); err != nil || !result.Ok || result.Binary != "1010.101" || result.Hex != "A.A" {
		t.Fatalf("10.625 = %+v, %v", result, err)
	}
	// A real (D20), kept as the programmer ans with its kind.
	if result, err := s.CalcEvaluateInt("1.0 / 4", 8, "dec", true); err != nil || !result.Ok || !result.Real || result.Value != "0.25" {
		t.Fatalf("1.0 / 4 = %+v, %v", result, err)
	}
	if result, err := s.CalcEvaluateInt("ans * 2", 8, "dec", true); err != nil || !result.Real || result.Value != "0.5" {
		t.Fatalf("a real ans * 2 = %+v, %v", result, err)
	}
	// The inspector's two directions (D21): 0x1e3 is the number 483 to Encode, and Decode reads bits.
	if view, err := s.CalcInspect("0x1e3", "binary16", "encode", 32, "dec", true); err != nil || !view.Ok || view.Bits != "0x5F8C" {
		t.Fatalf("encode 0x1e3 = %+v, %v", view, err)
	}
	if view, err := s.CalcInspect("0x3C00 | 0x8000", "binary16", "decode", 8, "dec", true); err != nil || !view.Ok || view.Value != "-1" {
		t.Fatalf("decode 0x3C00 | 0x8000 = %+v, %v", view, err)
	}
	s.DiscardPreview()
	if _, err := s.CalcEvaluate("1", "deg"); err == nil {
		t.Fatal("a discarded preview still evaluated")
	}
}

// A neural exam's preview evaluates vectors and matrices on the scientific panel (Phase 7), with a vector ans.
func TestTheExamPreviewEvaluatesNeuralMode(t *testing.T) {
	tools := &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific, calc.ModeNeural}}}
	detail := previewReply{ExamID: examID, Title: "Redes", Navigation: "free", Content: examdoc.Content{Exercises: []examdoc.Exercise{{ID: "1", Title: "Ejercicio 1"}}}, Tools: tools}
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/admin/exam/"+examID {
			reply(w, detail)
			return
		}
		if r.URL.Path == "/admin/sessions" {
			reply(w, roster())
			return
		}
		http.NotFound(w, r)
	})
	if _, err := s.PreviewExam(examID); err != nil {
		t.Fatal(err)
	}
	if result, err := s.CalcEvaluate("sigmoid([0, 0])", "rad"); err != nil || !result.Ok || result.Display != "[0.5, 0.5]" {
		t.Fatalf("sigmoid = %+v, %v", result, err)
	}
	if result, err := s.CalcEvaluate("[ans, ans]", "rad"); err != nil || !result.Ok || len(result.Rows) != 2 || result.Display != "[[0.5, 0.5], [0.5, 0.5]]" {
		t.Fatalf("[ans, ans] = %+v, %v", result, err)
	}
}

// An exam without a calculator loads, but nothing answers.
func TestTheExamPreviewWithoutACalculatorRefuses(t *testing.T) {
	detail := previewReply{ExamID: examID, Title: "Plain", Navigation: "free", Content: examdoc.Content{Exercises: []examdoc.Exercise{{ID: "1", Title: "Ejercicio 1"}}}}
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/admin/exam/"+examID {
			reply(w, detail)
			return
		}
		if r.URL.Path == "/admin/sessions" {
			reply(w, roster())
			return
		}
		http.NotFound(w, r)
	})
	view, err := s.PreviewExam(examID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Calculator != nil {
		t.Fatalf("an exam without tools grew a calculator: %+v", view.Calculator)
	}
	if got := s.CalcAllowedFunctions(); len(got) != 0 {
		t.Fatalf("allowed = %v", got)
	}
	if _, err := s.CalcEvaluate("1", "deg"); err == nil {
		t.Fatal("the preview calculator answered an exam without one")
	}
}

// Before anything is previewed there is no calculator at all.
func TestThePreviewCalculatorNeedsAPreviewedExam(t *testing.T) {
	s, _ := start(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/sessions" {
			reply(w, roster())
			return
		}
		http.NotFound(w, r)
	})
	if got := s.CalcAllowedFunctions(); len(got) != 0 {
		t.Fatalf("allowed = %v", got)
	}
	if _, err := s.CalcEvaluate("1", "deg"); err == nil {
		t.Fatal("nothing previewed, yet the calculator answered")
	}
}

func contains(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

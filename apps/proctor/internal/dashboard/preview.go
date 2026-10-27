package dashboard

import (
	"errors"
	"net/http"
	"slices"

	"exam-platform/shared/calc"
	"exam-platform/shared/examdoc"
)

// The exam preview dialog (owner decision 2026-10-23): the proctor opens a stored exam as the students see it,
// with the calculator working in it — the same embeddable component the student application hosts, evaluating
// through this service with the same shared/calc engine. A preview is not a session: nothing here is logged
// and no record is written; the preview's state lives only until another exam is previewed.

// ExamPreviewView is what the preview dialog shows: the exam as students see it, and whether it has a calculator.
type ExamPreviewView struct {
	ExamID     string             `json:"exam_id"`
	Title      string             `json:"title"`
	Navigation string             `json:"navigation"`
	Content    examdoc.Content    `json:"content"`
	Calculator *CalculatorSummary `json:"calculator"`
}

// CalcEvaluate evaluates one expression of the previewed exam's scientific panel, with vectors and matrices
// when the exam enables neural mode (Phase 7).
func (s *Service) CalcEvaluate(input string, angleMode string) (calc.CalcResult, error) {
	exam, err := s.previewCalculator()
	if err != nil {
		return calc.CalcResult{}, err
	}
	angleMode = calc.ResolveAngle(exam.angle, angleMode)
	result, evalErr := calc.Evaluate(input, calc.Options{AngleMode: angleMode, Allowed: exam.allowed, Ans: exam.ans, Neural: exam.neural})
	if evalErr != nil {
		var code *calc.Error
		if errors.As(evalErr, &code) {
			return calc.CalcResult{Ok: false, Code: code.Code}, nil
		}
		return calc.CalcResult{}, evalErr
	}
	exam.ans = result.Value
	return calc.ScientificResult(result), nil
}

// CalcEvaluateInt evaluates one programmer expression of the previewed exam, integer or real (D20).
func (s *Service) CalcEvaluateInt(input string, word int, inputBase string, signed bool) (calc.CalcIntResult, error) {
	exam, err := s.previewCalculator()
	if err != nil {
		return calc.CalcIntResult{}, err
	}
	base, ok := calc.InputBase(inputBase)
	if !ok {
		return calc.CalcIntResult{}, errors.New("The input base is not one of bin, oct, dec, hex.")
	}
	value, evalErr := calc.EvaluateProgrammer(input, calc.IntOptions{Word: word, Signed: signed, Base: base, Allowed: exam.allowed, Ans: exam.ansProgrammer})
	if evalErr != nil {
		var code *calc.Error
		if errors.As(evalErr, &code) {
			return calc.CalcIntResult{Ok: false, Code: code.Code}, nil
		}
		return calc.CalcIntResult{}, evalErr
	}
	exam.ansProgrammer = value
	result, _ := calc.ProgrammerResult(value, word)
	return result, nil
}

// CalcConvertFraction converts the shared input's value exactly, in the previewed exam's programmer panel (D6,
// D21).
func (s *Service) CalcConvertFraction(input string, word int, inputBase string, signed bool) (calc.CalcFractionResult, error) {
	exam, err := s.previewCalculator()
	if err != nil {
		return calc.CalcFractionResult{}, err
	}
	base, ok := calc.InputBase(inputBase)
	if !ok {
		return calc.CalcFractionResult{}, errors.New("The input base is not one of bin, oct, dec, hex.")
	}
	result, evalErr := calc.Fraction(input, calc.IntOptions{Word: word, Signed: signed, Base: base, Allowed: exam.allowed, Ans: exam.ansProgrammer})
	if evalErr != nil {
		var code *calc.Error
		if errors.As(evalErr, &code) {
			return calc.CalcFractionResult{Ok: false, Code: code.Code}, nil
		}
		return calc.CalcFractionResult{}, evalErr
	}
	return calc.CalcFractionResult{Ok: true, Binary: result.Binary, Octal: result.Octal, Hex: result.Hex}, nil
}

// CalcInspect encodes or decodes the shared input in one IEEE 754 format (plan Phases 6 and 6A, D13, D21) in
// the previewed exam's programmer panel; it answers as if the inspector did not exist when the exam disabled it.
func (s *Service) CalcInspect(input string, format string, direction string, word int, inputBase string, signed bool) (calc.CalcIEEEView, error) {
	state, err := s.previewCalculator()
	if err != nil {
		return calc.CalcIEEEView{}, err
	}
	s.mu.Lock()
	disabled := state.disabled
	s.mu.Unlock()
	if disabled["ieee754"] {
		return calc.CalcIEEEView{}, errors.New("The calculator is not available.")
	}
	base, ok := calc.InputBase(inputBase)
	if !ok {
		return calc.CalcIEEEView{}, errors.New("The input base is not one of bin, oct, dec, hex.")
	}
	view, evalErr := calc.Inspect(input, format, direction, calc.IntOptions{Word: word, Signed: signed, Base: base, Allowed: state.allowed, Ans: state.ansProgrammer})
	if evalErr != nil {
		var code *calc.Error
		if errors.As(evalErr, &code) {
			return calc.CalcIEEEView{Ok: false, Code: code.Code}, nil
		}
		return calc.CalcIEEEView{}, evalErr
	}
	return calc.CalcIEEEView{Ok: true, IEEEView: view}, nil
}

// previewCalculator is the previewed exam's calculator state; an exam without one has no calculator to evaluate
// with. The caller holds s.mu.
func (s *Service) previewCalculator() (*previewCalculatorState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil {
		return nil, errors.New("Choose an exam to preview first.")
	}
	return s.preview, nil
}

// previewCalculatorState is one previewed exam's resolved calculator: the allowed set and the ans values,
// recomputed when another exam is previewed.
type previewCalculatorState struct {
	angle    string
	allowed  map[string]bool
	disabled map[string]bool
	// neural is whether the exam enables neural mode, whose vectors and matrices the scientific panel accepts.
	neural bool
	// ans is the scientific panel's ans: a number, a vector or a matrix.
	ans calc.Quantity
	// ansProgrammer is the programmer ans with its kind, an integer's bits or a real's exact value.
	ansProgrammer calc.Value
}

// PreviewExam loads a stored exam for the preview dialog and keeps its calculator state until another exam is
// previewed.
func (s *Service) PreviewExam(examID string) (ExamPreviewView, error) {
	ctx, base, token, _, err := s.adminConnection()
	if err != nil {
		return ExamPreviewView{}, err
	}
	var detail struct {
		ExamID     string          `json:"exam_id"`
		Title      string          `json:"title"`
		Navigation string          `json:"navigation"`
		Content    examdoc.Content `json:"content"`
		Tools      *calc.Tools     `json:"tools,omitempty"`
	}
	if err := adminSend(ctx, s.client, base, token, http.MethodGet, "/admin/exam/"+examID, "application/json", nil, &detail); err != nil {
		return ExamPreviewView{}, err
	}
	view := ExamPreviewView{ExamID: detail.ExamID, Title: detail.Title, Navigation: detail.Navigation, Content: detail.Content}
	s.mu.Lock()
	s.preview = nil
	s.mu.Unlock()
	if detail.Tools != nil && detail.Tools.On() {
		effective := detail.Tools.Effective()
		summary := &CalculatorSummary{Modes: effective.Calculator.Modes, Angle: effective.Calculator.AngleMode, Disabled: len(effective.Calculator.DisabledFunctions), DisabledNames: effective.Calculator.DisabledFunctions}
		view.Calculator = summary
		state := &previewCalculatorState{angle: effective.Calculator.AngleMode, allowed: calc.Allowed(effective), disabled: map[string]bool{},
			neural: calc.ModeEnabled(effective, calc.ModeNeural)}
		for _, name := range effective.Calculator.DisabledFunctions {
			state.disabled[name] = true
		}
		s.mu.Lock()
		s.preview = state
		s.mu.Unlock()
	}
	return view, nil
}

// CalcAllowedFunctions returns the previewed exam's allowed names; empty when nothing is previewed or the exam
// has no calculator.
func (s *Service) CalcAllowedFunctions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preview == nil {
		return []string{}
	}
	names := make([]string, 0, len(s.preview.allowed))
	for name := range s.preview.allowed {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// discardPreview forgets the previewed exam's calculator state; the dialog's close calls it.
func (s *Service) discardPreview() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preview = nil
}

// DiscardPreview forgets the previewed exam; called when the preview dialog closes.
func (s *Service) DiscardPreview() { s.discardPreview() }

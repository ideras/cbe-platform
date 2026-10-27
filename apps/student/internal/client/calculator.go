package client

import (
	"errors"
	"slices"
	"time"

	"exam-platform/shared/calc"
)

// clientTimestamp is the client_time the tool events carry, in the same shape as the monitor's events.
func clientTimestamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// errCalcPaused is the refusal while the log queue cannot take a record (D12); the UI shows its text as-is.
var errCalcPaused = errors.New("The calculator is paused until your calculations reach the server. Check your connection.")

// CalcResult, CalcIntResult and CalcFractionResult are the shared results of shared/calc; the student client
// keeps the names it has always used.
type CalcResult = calc.CalcResult

// CalcAllowedFunctions returns the names the calculator may evaluate, from calc.Allowed on the exam's effective
// tools configuration (D8, D16). It is empty when the calculator is off, the exam is not on the screen, or the
// exam has ended. The frontend never computes this set itself.
func (s *Service) CalcAllowedFunctions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, ok := s.calculatorConfig()
	if !ok {
		return []string{}
	}
	allowed := calc.Allowed(config.Effective())
	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// CalcEvaluate evaluates one expression of the scientific panel, which neural mode shares (Phase 7): numbers,
// and vectors and matrices when the exam enables neural mode. It keeps the panel's ans (D10), which may hold a
// vector or a matrix, and logs the evaluation under the panel's name, scientific or neural. angleMode is used only when
// the exam's configuration leaves the choice to the student (D3); otherwise the configured mode decides,
// whatever the frontend sends. The evaluation is logged inside this method, before its result is returned, so
// the frontend cannot evaluate without logging (D4).
func (s *Service) CalcEvaluate(input string, angleMode string) (CalcResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, ok := s.calculatorConfig()
	if !ok {
		return CalcResult{}, errors.New("The calculator is not available.")
	}
	if s.tools.full() {
		// An evaluation that cannot be logged is refused (D12): the calculator pauses until the queue drains,
		// and nothing is dropped silently.
		return CalcResult{}, errors.New("The calculator is paused until your calculations reach the server. Check your connection.")
	}
	effective := config.Effective()
	angleMode = calc.ResolveAngle(effective.Calculator.AngleMode, angleMode)
	mode := calc.ScientificPanel(effective)
	result, err := calc.Evaluate(input, calc.Options{AngleMode: angleMode, Allowed: calc.Allowed(effective), Ans: s.calcAns, Neural: calc.ModeEnabled(effective, calc.ModeNeural)})
	if err != nil {
		var code *calc.Error
		if errors.As(err, &code) {
			if !s.tools.enqueue("calc_evaluated", map[string]any{"client_time": clientTimestamp(), "mode": mode, "expression": input, "ok": false, "code": code.Code, "angle_mode": angleMode}) {
				return CalcResult{}, errCalcPaused
			}
			return CalcResult{Ok: false, Code: code.Code}, nil
		}
		return CalcResult{}, err
	}
	detail := map[string]any{"client_time": clientTimestamp(), "mode": mode, "expression": input, "ok": true, "display": result.Display, "angle_mode": angleMode}
	if !s.tools.enqueue("calc_evaluated", detail) {
		return CalcResult{}, errCalcPaused
	}
	s.calcAns = result.Value
	return calc.ScientificResult(result), nil
}

// CalcOpened, CalcClosed and CalcModeChanged record the calculator's usage events (D12). They are gated on the
// exam having a calculator, not on the exam being unlocked: closing at a lock is exactly the moment worth
// recording. The frontend ignores their failures, which happen only when the log queue is full.
func (s *Service) CalcOpened(mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.calculatorEnabled() {
		return errors.New("The calculator is not available.")
	}
	if !s.tools.enqueue("calc_opened", map[string]any{"client_time": clientTimestamp(), "mode": mode}) {
		return errCalcPaused
	}
	return nil
}

func (s *Service) CalcClosed(mode string, openMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.calculatorEnabled() {
		return errors.New("The calculator is not available.")
	}
	openMs = min(max(openMs, 0), 24*int64(time.Hour)/int64(time.Millisecond))
	if !s.tools.enqueue("calc_closed", map[string]any{"client_time": clientTimestamp(), "mode": mode, "open_ms": openMs}) {
		return errCalcPaused
	}
	return nil
}

func (s *Service) CalcModeChanged(from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.calculatorEnabled() {
		return errors.New("The calculator is not available.")
	}
	if !s.tools.enqueue("calc_mode_changed", map[string]any{"client_time": clientTimestamp(), "from": from, "to": to}) {
		return errCalcPaused
	}
	return nil
}

// CalcInspect encodes or decodes the shared input in one IEEE 754 format (plan Phases 6 and 6A, D13, D21):
// direction is "encode" or "decode", and the panel's word size, input base and signed/unsigned toggle are the
// caller's, since the input is a programmer expression. It answers as if the inspector did not exist when the
// calculator is unavailable, programmer mode is off, or the exam disabled the inspector: the panel is hidden and
// this method refuses. Every inspection is logged as calc_inspected, with what reproduces it.
func (s *Service) CalcInspect(input string, format string, direction string, word int, inputBase string, signed bool) (calc.CalcIEEEView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, ok := s.calculatorConfig()
	if !ok || !s.calculatorModeEnabled(calc.ModeProgrammer) || slices.Contains(config.Effective().Calculator.DisabledFunctions, "ieee754") {
		return calc.CalcIEEEView{}, errors.New("The calculator is not available.")
	}
	if s.tools.full() {
		return calc.CalcIEEEView{}, errCalcPaused
	}
	base, ok := intBase(inputBase)
	if !ok {
		return calc.CalcIEEEView{}, errors.New("The input base is not one of bin, oct, dec, hex.")
	}
	detail := map[string]any{"client_time": clientTimestamp(), "format": format, "direction": direction, "input": input,
		"word_size": word, "input_base": inputBase, "signed": signed}
	view, err := calc.Inspect(input, format, direction, calc.IntOptions{Word: word, Signed: signed, Base: base, Allowed: calc.Allowed(config.Effective()), Ans: s.calcAnsProgrammer})
	if err != nil {
		var code *calc.Error
		if errors.As(err, &code) {
			detail["ok"], detail["code"] = false, code.Code
			if !s.tools.enqueue("calc_inspected", detail) {
				return calc.CalcIEEEView{}, errCalcPaused
			}
			return calc.CalcIEEEView{Ok: false, Code: code.Code}, nil
		}
		return calc.CalcIEEEView{}, err
	}
	detail["ok"], detail["bits"] = true, view.Bits
	if !s.tools.enqueue("calc_inspected", detail) {
		return calc.CalcIEEEView{}, errCalcPaused
	}
	return calc.CalcIEEEView{Ok: true, IEEEView: view}, nil
}

// CalcTabChanged records a change between the programmer panel's tabs, keypad, ieee754 and fraction (D22). Like
// CalcModeChanged it carries no expression text, and the frontend ignores its failure.
func (s *Service) CalcTabChanged(from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.calculatorEnabled() {
		return errors.New("The calculator is not available.")
	}
	if !s.tools.enqueue("calc_tab_changed", map[string]any{"client_time": clientTimestamp(), "from": from, "to": to}) {
		return errCalcPaused
	}
	return nil
}

// calculatorEnabled says the exam has a calculator at all — without the phase and lock checks of
// calculatorConfig, so the events of opening and closing at a lock or an end are still recorded.
func (s *Service) calculatorEnabled() bool {
	return s.exam != nil && s.exam.Tools != nil && s.exam.Tools.On()
}

// calculatorConfig is the exam's tools configuration while the exam itself is on the screen (D11): not on the
// login or loading screens, not while the exam is locked, not after it ended. The caller holds s.mu.
func (s *Service) calculatorConfig() (calc.Tools, bool) {
	if s.phase != "exam" || s.exam == nil || s.exam.Tools == nil || !s.exam.Tools.On() || s.examLocked() {
		return calc.Tools{}, false
	}
	return *s.exam.Tools, true
}

type CalcIntResult = calc.CalcIntResult

type CalcFractionResult = calc.CalcFractionResult

// CalcEvaluateInt evaluates one programmer expression (plan Phases 5 and 6A), integer or real (D20), and keeps
// the programmer ans with its kind. The panel's word size, signed/unsigned toggle and input base are the
// caller's; the logging follows the same rules as CalcEvaluate, with word_size, input_base and signed in the
// detail so the result can be reproduced.
func (s *Service) CalcEvaluateInt(input string, word int, inputBase string, signed bool) (CalcIntResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, ok := s.calculatorConfig()
	if !ok || !s.calculatorModeEnabled(calc.ModeProgrammer) {
		return CalcIntResult{}, errors.New("The calculator is not available.")
	}
	if s.tools.full() {
		return CalcIntResult{}, errCalcPaused
	}
	base, ok := intBase(inputBase)
	if !ok {
		return CalcIntResult{}, errors.New("The input base is not one of bin, oct, dec, hex.")
	}
	detail := map[string]any{"client_time": clientTimestamp(), "mode": "programmer", "expression": input, "word_size": word, "input_base": inputBase, "signed": signed}
	value, err := calc.EvaluateProgrammer(input, calc.IntOptions{Word: word, Signed: signed, Base: base, Allowed: calc.Allowed(config.Effective()), Ans: s.calcAnsProgrammer})
	if err != nil {
		var code *calc.Error
		if errors.As(err, &code) {
			detail["ok"], detail["code"] = false, code.Code
			if !s.tools.enqueue("calc_evaluated", detail) {
				return CalcIntResult{}, errCalcPaused
			}
			return CalcIntResult{Ok: false, Code: code.Code}, nil
		}
		return CalcIntResult{}, err
	}
	result, display := calc.ProgrammerResult(value, word)
	detail["ok"], detail["display"] = true, display
	if !s.tools.enqueue("calc_evaluated", detail) {
		return CalcIntResult{}, errCalcPaused
	}
	s.calcAnsProgrammer = value
	return result, nil
}

// CalcConvertFraction reads the shared input as a programmer expression and converts its value to binary,
// octal and hexadecimal, exactly (D6, D21). It is logged like an evaluation, with view "fraction" and what
// reproduces it.
func (s *Service) CalcConvertFraction(input string, word int, inputBase string, signed bool) (CalcFractionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, ok := s.calculatorConfig()
	if !ok || !s.calculatorModeEnabled(calc.ModeProgrammer) {
		return CalcFractionResult{}, errors.New("The calculator is not available.")
	}
	if s.tools.full() {
		return CalcFractionResult{}, errCalcPaused
	}
	base, ok := intBase(inputBase)
	if !ok {
		return CalcFractionResult{}, errors.New("The input base is not one of bin, oct, dec, hex.")
	}
	detail := map[string]any{"client_time": clientTimestamp(), "mode": "programmer", "view": "fraction", "expression": input, "word_size": word, "input_base": inputBase, "signed": signed}
	result, err := calc.Fraction(input, calc.IntOptions{Word: word, Signed: signed, Base: base, Allowed: calc.Allowed(config.Effective()), Ans: s.calcAnsProgrammer})
	if err != nil {
		var code *calc.Error
		if errors.As(err, &code) {
			detail["ok"], detail["code"] = false, code.Code
			if !s.tools.enqueue("calc_evaluated", detail) {
				return CalcFractionResult{}, errCalcPaused
			}
			return CalcFractionResult{Ok: false, Code: code.Code}, nil
		}
		return CalcFractionResult{}, err
	}
	detail["ok"], detail["display"], detail["binary"], detail["octal"], detail["hex"] = true, result.Binary, result.Binary, result.Octal, result.Hex
	if !s.tools.enqueue("calc_evaluated", detail) {
		return CalcFractionResult{}, errCalcPaused
	}
	return CalcFractionResult{Ok: true, Binary: result.Binary, Octal: result.Octal, Hex: result.Hex}, nil
}

// calculatorModeEnabled says the mode is among the exam's enabled ones.
func (s *Service) calculatorModeEnabled(mode string) bool {
	return slices.Contains(s.exam.Tools.Calculator.Modes, mode) || len(s.exam.Tools.Calculator.Modes) == 0 && mode == calc.ModeScientific
}

// intBase reads the panel's base selector.
func intBase(inputBase string) (int, bool) { return calc.InputBase(inputBase) }

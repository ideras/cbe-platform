package client

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"exam-platform/shared/calc"
)

// calc_toolQueueLimitForTest is the tool queue's own limit (D12); the test fills the queue to its edge.
const calc_toolQueueLimitForTest = 1000

// The calculator is available exactly while the exam itself is on the screen (D11): not on the login or loading
// screens, not while locked, not after the end. Its methods expose the allowed set and evaluate, and the ans of
// the scientific mode survives from evaluation to evaluation.
func TestTheCalculatorIsOnlyAvailableWhileTheExamIsShown(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}}}
	s := newClient(t, f, nil)
	if got := s.CalcAllowedFunctions(); len(got) != 0 {
		t.Fatalf("allowed functions before login = %v", got)
	}
	if _, err := s.CalcEvaluate("1+1", "deg"); err == nil {
		t.Fatal("the calculator answered before the exam was shown")
	}
	loginClient(t, s, f)
	if _, err := s.CalcEvaluate("2+3*4", "deg"); err != nil {
		t.Fatalf("evaluate = %v", err)
	}
	if names := s.CalcAllowedFunctions(); !slices.Contains(names, "sin") || slices.Contains(names, "matmul") {
		t.Fatalf("allowed functions = %v", names)
	}
	// Locked: nothing answers, and the codes do not leak through Go errors.
	s.mu.Lock()
	s.locked = &Lock{Since: f.status.ServerTime, Reason: "away"}
	s.mu.Unlock()
	if got := s.CalcAllowedFunctions(); len(got) != 0 {
		t.Fatalf("allowed functions while locked = %v", got)
	}
	if _, err := s.CalcEvaluate("1", "deg"); err == nil {
		t.Fatal("the calculator answered while the exam was locked")
	}
	s.mu.Lock()
	s.locked = nil
	s.mu.Unlock()
	if _, err := s.CalcEvaluate("1", "deg"); err != nil {
		t.Fatalf("evaluate after the unlock = %v", err)
	}
}

// A failed evaluation is a result with a code, never a Go error; the frontend maps the code to words.
func TestCalcEvaluateReturnsCodesNotErrors(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}, DisabledFunctions: []string{"sinh"}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	for input, want := range map[string]string{
		"1/0":       calc.CodeDivisionByZero,
		"foo(1)":    calc.CodeUnknownFunction,
		"foo":       calc.CodeUnknownIdent,
		"sinh(1)":   calc.CodeUnknownFunction, // disabled: never ARITY, never a Go error
		"sinh(1,2)": calc.CodeUnknownFunction,
		"sin(30":    calc.CodeSyntax,
	} {
		result, err := s.CalcEvaluate(input, "deg")
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if result.Ok || result.Code != want {
			t.Fatalf("%q = %+v, want ok:false code:%s", input, result, want)
		}
	}
}

// The ans of the scientific mode lives in the Go service, with full precision, and is read by the next
// evaluation (D10).
func TestCalcEvaluateKeepsTheAnsOfTheMode(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	first, err := s.CalcEvaluate("1/3", "deg")
	if err != nil || !first.Ok {
		t.Fatalf("1/3 = %+v, %v", first, err)
	}
	second, err := s.CalcEvaluate("ans*3", "deg")
	if err != nil || !second.Ok {
		t.Fatalf("ans*3 = %+v, %v", second, err)
	}
	// Full precision: 3·(1/3) in the floats, not the rounded 0.333333333333 of the display.
	if second.Display != "1" {
		t.Fatalf("ans*3 = %+v", second)
	}
}

// Neural mode shares the scientific panel (Phase 7): vectors and matrices evaluate, ans keeps a vector, a matrix
// comes with its rows, and the log records the one-line display under the panel's name — neural when the exam
// enables neural mode alone. Without neural mode a bracket is SYNTAX.
func TestCalcEvaluateWithNeuralMode(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeNeural}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	first, err := s.CalcEvaluate("softmax([1000, 1000])", "deg")
	if err != nil || !first.Ok || first.Display != "[0.5, 0.5]" || first.Rows != nil {
		t.Fatalf("softmax = %+v, %v", first, err)
	}
	second, err := s.CalcEvaluate("[ans, ans * 2]", "deg")
	if err != nil || !second.Ok || second.Display != "[[0.5, 0.5], [1, 1]]" || len(second.Rows) != 2 || second.Rows[1][0] != "1" {
		t.Fatalf("[ans, ans * 2] = %+v, %v", second, err)
	}
	failed, err := s.CalcEvaluate("dot([1, 2], [1])", "deg")
	if err != nil || failed.Ok || failed.Code != calc.CodeDimMismatch {
		t.Fatalf("dot = %+v, %v", failed, err)
	}
	until := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		var details []map[string]any
		for _, record := range f.telemetry {
			if record.kind == "calc_evaluated" {
				details = append(details, record.detail)
			}
		}
		f.mu.Unlock()
		if len(details) == 3 {
			if details[1]["display"] != "[[0.5, 0.5], [1, 1]]" || details[1]["mode"] != calc.ModeNeural || details[2]["code"] != calc.CodeDimMismatch {
				t.Fatalf("the neural details are wrong: %v", details)
			}
			break
		}
		if time.Now().After(until) {
			t.Fatalf("the evaluations were not logged: %v", details)
		}
		time.Sleep(2 * time.Millisecond)
	}

	scientific := newFixture(t)
	scientific.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}}}
	plain := newClient(t, scientific, nil)
	loginClient(t, plain, scientific)
	if result, err := plain.CalcEvaluate("[1, 2]", "deg"); err != nil || result.Code != calc.CodeSyntax {
		t.Fatalf("[1, 2] without neural mode = %+v, %v", result, err)
	}
}

// The configured angle mode wins over what the frontend sends; only a user-selectable mode listens (D3).
func TestCalcEvaluateFollowsTheConfiguredAngleMode(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}, AngleMode: calc.AngleDegrees}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if result, err := s.CalcEvaluate("sin(90)", "rad"); err != nil || !result.Ok || result.Display != "1" {
		t.Fatalf("sin(90) in a deg exam = %+v, %v", result, err)
	}
	// And a user-selectable exam follows the frontend's choice: sin(pi/6) is 0.5 in rad, and sin(pi/6 degrees)
	// would not be.
	f2 := newFixture(t)
	f2.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}, AngleMode: calc.AngleUser}}
	s2 := newClient(t, f2, nil)
	loginClient(t, s2, f2)
	if result, err := s2.CalcEvaluate("sin(pi/6)", "rad"); err != nil || !result.Ok || result.Display != "0.5" {
		t.Fatalf("sin(pi/6) in a user exam told rad = %+v, %v", result, err)
	}
}

// A calculator that is off answers unavailable even when the exam is on the screen.
func TestCalcEvaluateRefusesWhenTheCalculatorIsOff(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if got := s.CalcAllowedFunctions(); len(got) != 0 {
		t.Fatalf("allowed functions without tools = %v", got)
	}
	if _, err := s.CalcEvaluate("1", "deg"); err == nil {
		t.Fatal("the calculator answered an exam without tools")
	}
}

// The tool events go on the calculator's own queue (D12) with the documented details, and every evaluation —
// successful or not — is queued with the expression exactly as typed (D4).
func TestCalcEventsFlowOnTheirOwnQueue(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}, AngleMode: calc.AngleDegrees}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if err := s.CalcOpened("scientific"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CalcEvaluate("2+3*4", "deg"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CalcEvaluate("log(8) ", "deg"); err != nil { // exactly as typed, trailing space and all
		t.Fatal(err)
	}
	if err := s.CalcClosed("scientific", 1500); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		kinds := []string{}
		for _, record := range f.telemetry {
			if record.path == "/event" && strings.HasPrefix(record.kind, "calc_") {
				kinds = append(kinds, record.kind)
			}
		}
		details := map[string]map[string]any{}
		for _, record := range f.telemetry {
			if record.kind == "calc_evaluated" {
				details[record.detail["expression"].(string)] = record.detail
			}
		}
		f.mu.Unlock()
		if slices.Equal(kinds, []string{"calc_opened", "calc_evaluated", "calc_evaluated", "calc_closed"}) && len(details) == 2 {
			if details["2+3*4"]["display"] != "14" || details["2+3*4"]["angle_mode"] != "deg" || details["2+3*4"]["ok"] != true {
				t.Fatalf("the successful evaluation's detail is wrong: %v", details["2+3*4"])
			}
			if details["log(8) "]["code"] != calc.CodeArity || details["log(8) "]["ok"] != false || details["log(8) "]["expression"] != "log(8) " {
				t.Fatalf("the failed evaluation's detail is wrong: %v", details["log(8) "])
			}
			break
		}
		if time.Now().After(until) {
			t.Fatalf("the events did not arrive in order: %v", kinds)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// A queue that cannot drain stops evaluations with the pause message, and nothing is dropped (D12): the queued
// events still arrive once the server answers again.
func TestTheFullLogQueueRefusesEvaluations(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}}}
	release := make(chan struct{})
	f.onTelemetry = func(w http.ResponseWriter, r *http.Request, record telemetryRecord) bool {
		if strings.HasPrefix(record.kind, "calc_") {
			<-release // hold the sends so the queue cannot drain while the test fills it
		}
		return false
	}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	// Fill the queue to its limit while the send loop is held.
	s.mu.Lock()
	for i := 0; i < calc_toolQueueLimitForTest; i++ {
		if !s.tools.enqueue("calc_opened", map[string]any{"mode": "scientific"}) {
			t.Fatalf("the queue refused event %d before its limit", i+1)
		}
	}
	full := s.tools.full()
	s.mu.Unlock()
	if !full {
		t.Fatal("the queue did not fill")
	}
	_, err := s.CalcEvaluate("1+1", "deg")
	if err == nil || err.Error() != "The calculator is paused until your calculations reach the server. Check your connection." {
		t.Fatalf("a full queue = %v, want the pause message", err)
	}
	// Drain and resume: no event was dropped.
	close(release)
	until := time.Now().Add(5 * time.Second)
	for {
		queued := s.tools.queued()
		if queued == 0 {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("%d events never drained", queued)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := s.CalcEvaluate("1+1", "deg"); err != nil {
		t.Fatalf("after the drain = %v", err)
	}
}

// A burst of evaluations cannot delay a focus loss: the queues are separate, so the focus event is sent while
// the calculator's events are still being worked off (D12).
func TestACalcBurstDoesNotDelayTheFocusLoss(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}}}
	release := make(chan struct{})
	f.onTelemetry = func(w http.ResponseWriter, r *http.Request, record telemetryRecord) bool {
		if strings.HasPrefix(record.kind, "calc_") {
			<-release // hold every calculator event until the focus loss is long since recorded
		}
		return false
	}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	s.mu.Lock()
	for i := 0; i < 40; i++ {
		s.tools.enqueue("calc_evaluated", map[string]any{"expression": "1"})
	}
	s.mu.Unlock()
	before := time.Now()
	ReportNativeFocus(s, false)
	waitView(t, s, func(v View) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, record := range f.telemetry {
			if record.kind == "focus_lost" && record.at.After(before) {
				return true
			}
		}
		return false
	})
	close(release)
}

// The end of the exam clears the calculator's ans; a lock does not (D11).
func TestTheExamEndClearsTheAns(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if _, err := s.CalcEvaluate("7*6", "deg"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if s.calcAns.Number() != 42 {
		t.Fatalf("ans = %v", s.calcAns)
	}
	s.mu.Unlock()
	// The lock keeps the ans.
	s.mu.Lock()
	s.locked = &Lock{Since: f.status.ServerTime, Reason: "away"}
	s.mu.Unlock()
	s.mu.Lock()
	if s.calcAns.Number() != 42 {
		t.Fatal("the lock cleared the ans")
	}
	s.mu.Unlock()
	// The submission clears it.
	s.mu.Lock()
	s.locked = nil
	s.mu.Unlock()
	if _, err := s.Submit(map[string]string{"q1": "done", "q2": ""}, 1); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Phase == "complete" })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calcAns.Shape != calc.ShapeNumber || s.calcAns.Number() != 0 {
		t.Fatalf("the exam's end left the ans: %v", s.calcAns)
	}
}

// The programmer evaluator through the service: the panel's word size, signed toggle and input base decide,
// the programmer ans survives evaluations with its kind, and the details carry word_size, input_base and signed.
func TestCalcEvaluateIntFollowsThePanel(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific, calc.ModeProgrammer}, AngleMode: calc.AngleUser}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	result, err := s.CalcEvaluateInt("0xFF + 1", 8, "hex", true)
	if err != nil || !result.Ok {
		t.Fatalf("0xFF + 1 = %+v, %v", result, err)
	}
	if result.Unsigned != "0" || result.Signed != "0" || result.Hex != "0x00" || result.Binary != "00000000" || !result.Carry || result.Overflow {
		t.Fatalf("0xFF + 1 at 8 bits = %+v", result)
	}
	// The programmer ans kept the raw bits; the next evaluation reads it.
	if _, err := s.CalcEvaluateInt("ans", 8, "hex", true); err != nil {
		t.Fatal(err)
	}
	// The unsigned reading of -8 >> 1 differs by the toggle: 124 when unsigned, -4 when signed.
	unsigned, err := s.CalcEvaluateInt("-8 >> 1", 8, "dec", false)
	if err != nil || unsigned.Signed != "124" || unsigned.Binary != "01111100" {
		t.Fatalf("-8 >> 1 unsigned = %+v, %v", unsigned, err)
	}
	signedOnly, err := s.CalcEvaluateInt("-8 >> 1", 8, "dec", true)
	if err != nil || signedOnly.Signed != "-4" || signedOnly.Binary != "11111100" {
		t.Fatalf("-8 >> 1 signed = %+v, %v", signedOnly, err)
	}
	// A real (D20) answers only its display, and the ans keeps it as a real.
	real, err := s.CalcEvaluateInt("1.0 / 4", 8, "dec", true)
	if err != nil || !real.Ok || !real.Real || real.Value != "0.25" || real.Unsigned != "" || real.Carry {
		t.Fatalf("1.0 / 4 = %+v, %v", real, err)
	}
	if again, err := s.CalcEvaluateInt("ans + 1", 8, "dec", true); err != nil || !again.Real || again.Value != "1.25" {
		t.Fatalf("a real ans + 1 = %+v, %v", again, err)
	}
	if refused, err := s.CalcEvaluateInt("ans & 1", 8, "dec", true); err != nil || refused.Ok || refused.Code != calc.CodeNotInteger {
		t.Fatalf("a real ans & 1 = %+v, %v", refused, err)
	}
	if _, err := s.CalcEvaluateInt("-8 >> 1", 8, "dec", true); err != nil {
		t.Fatal(err)
	}
	// The details reproduce the result; the queue sends asynchronously, so poll until the last one arrives.
	until := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		var last map[string]any
		for _, record := range f.telemetry {
			if record.kind == "calc_evaluated" && record.detail["expression"] == "-8 >> 1" && record.detail["signed"] == true {
				last = record.detail
			}
		}
		f.mu.Unlock()
		if last != nil {
			if last["word_size"] != float64(8) || last["input_base"] != "dec" || last["signed"] != true || last["mode"] != "programmer" {
				t.Fatalf("the detail cannot reproduce the result: %v", last)
			}
			return
		}
		if time.Now().After(until) {
			t.Fatalf("the programmer details never arrived: %v", last)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The programmer panel is unavailable when the exam enables only the scientific mode.
func TestCalcEvaluateIntNeedsProgrammerMode(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if _, err := s.CalcEvaluateInt("1+1", 8, "dec", true); err == nil {
		t.Fatal("the programmer panel answered a scientific-only exam")
	}
	if _, err := s.CalcConvertFraction("10.625", 32, "dec", true); err == nil {
		t.Fatal("the fraction field answered a scientific-only exam")
	}
}

// The Radix Point tab converts the shared input exactly (D6, D21) and is logged with view "fraction" and the panel
// settings that reproduce it.
func TestCalcConvertFractionLogsTheView(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeProgrammer}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	result, err := s.CalcConvertFraction("10.625", 32, "dec", false)
	if err != nil || !result.Ok || result.Binary != "1010.101" || result.Octal != "12.5" || result.Hex != "A.A" {
		t.Fatalf("10.625 = %+v, %v", result, err)
	}
	until := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		found := false
		for _, record := range f.telemetry {
			if record.kind == "calc_evaluated" && record.detail["view"] == "fraction" && record.detail["expression"] == "10.625" && record.detail["mode"] == "programmer" &&
				record.detail["word_size"] == float64(32) && record.detail["input_base"] == "dec" && record.detail["signed"] == false {
				found = true
			}
		}
		f.mu.Unlock()
		if found {
			return
		}
		if time.Now().After(until) {
			t.Fatal("the fraction conversion was not logged with view fraction")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// The IEEE 754 inspector through the service: it answers the reading and logs calc_inspected with the bits, the
// direction and the panel settings, and it answers as if it did not exist when the exam disabled it (plan
// Phases 6 and 6A).
func TestCalcInspectAnswersAndLogs(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeProgrammer}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	view, err := s.CalcInspect("1.0", "binary16", calc.DirectionEncode, 32, "dec", true)
	if err != nil || !view.Ok || view.Bits != "0x3C00" || view.Class != "normal" || view.Value != "1" {
		t.Fatalf("1.0 in binary16 = %+v, %v", view, err)
	}
	if view, err = s.CalcInspect("0x412A0000", "binary16", calc.DirectionDecode, 32, "dec", true); err != nil || view.Ok || view.Code != calc.CodeBitWidth {
		t.Fatalf("a wide pattern = %+v, %v, want a BIT_WIDTH result", view, err)
	}
	if _, err = s.CalcInspect("1.0", "binary16", calc.DirectionEncode, 32, "base 3", true); err == nil {
		t.Fatal("an unknown input base was accepted")
	}
	until := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		found, foundFailure := false, false
		for _, record := range f.telemetry {
			if record.kind != "calc_inspected" {
				continue
			}
			reproducible := record.detail["word_size"] == float64(32) && record.detail["input_base"] == "dec" && record.detail["signed"] == true
			if record.detail["bits"] == "0x3C00" && record.detail["input"] == "1.0" && record.detail["direction"] == "encode" && reproducible {
				found = true
			}
			if record.detail["code"] == "BIT_WIDTH" && record.detail["input"] == "0x412A0000" && record.detail["direction"] == "decode" && reproducible {
				foundFailure = true
			}
		}
		f.mu.Unlock()
		if found && foundFailure {
			return
		}
		if time.Now().After(until) {
			t.Fatalf("the inspections were not logged: found=%v failed=%v", found, foundFailure)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// With the inspector disabled, the panel's method answers as if it did not exist.
func TestCalcInspectRefusesWhenDisabled(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeProgrammer}, DisabledFunctions: []string{"ieee754"}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if view, err := s.CalcInspect("1.0", "binary16", calc.DirectionEncode, 32, "dec", true); err == nil || view.Ok {
		t.Fatalf("a disabled inspector = %+v, %v", view, err)
	}
	// And scientific-only exams have no programmer panel at all.
	f2 := newFixture(t)
	f2.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeScientific}}}
	s2 := newClient(t, f2, nil)
	loginClient(t, s2, f2)
	if _, err := s2.CalcInspect("1.0", "binary16", calc.DirectionEncode, 32, "dec", true); err == nil {
		t.Fatal("the inspector answered a scientific-only exam")
	}
}

// A tab change is logged as calc_tab_changed with from and to, and no expression text (D22).
func TestCalcTabChangedIsLoggedWithoutTheInput(t *testing.T) {
	f := newFixture(t)
	f.examTools = &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: []string{calc.ModeProgrammer}}}
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if err := s.CalcTabChanged("keypad", "ieee754"); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	for {
		f.mu.Lock()
		var detail map[string]any
		for _, record := range f.telemetry {
			if record.kind == "calc_tab_changed" {
				detail = record.detail
			}
		}
		f.mu.Unlock()
		if detail != nil {
			if detail["from"] != "keypad" || detail["to"] != "ieee754" || detail["client_time"] == nil {
				t.Fatalf("calc_tab_changed = %v", detail)
			}
			for _, key := range []string{"expression", "input"} {
				if _, ok := detail[key]; ok {
					t.Fatalf("calc_tab_changed carries %s: %v", key, detail)
				}
			}
			return
		}
		if time.Now().After(until) {
			t.Fatal("the tab change was not logged")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// Without a calculator, a tab change is refused like every calculator event.
func TestCalcTabChangedNeedsTheCalculator(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if err := s.CalcTabChanged("keypad", "fraction"); err == nil {
		t.Fatal("a tab change was accepted for an exam without a calculator")
	}
}

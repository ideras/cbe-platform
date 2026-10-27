package examdoc

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"exam-platform/shared/calc"
)

// calculatorSource builds an exam file whose front matter carries the given calculator value after the usual
// keys. The calculator key is line 4; the exercise and its answer field are lines 5 and 6.
func calculatorSource(value string) string {
	return "---\ntitle: Quiz\nduration: 30m\ncalculator: " + value + "\n---\n## A\n{{x}}\n"
}

func calculatorExam(t *testing.T, value string) Exam {
	t.Helper()
	exam, err := Compile([]byte(calculatorSource(value)))
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	return exam
}

func TestCalculatorKeyTurnsTheCalculatorOn(t *testing.T) {
	exam := calculatorExam(t, "")
	if exam.Tools == nil || !exam.Tools.On() {
		t.Fatalf("the bare calculator key did not turn the calculator on: %+v", exam.Tools)
	}
	if exam.Tools.Version != 1 || exam.Tools.Calculator.Modes != nil || exam.Tools.Calculator.AngleMode != "" || exam.Tools.Calculator.DisabledFunctions != nil {
		t.Fatalf("the bare calculator key wrote more than it was given: %+v", exam.Tools.Calculator)
	}
	// Effective applies the defaults, the way the server sends the config to the student.
	effective := exam.Tools.Effective()
	if !slices.Equal(effective.Calculator.Modes, []string{calc.ModeScientific}) || effective.Calculator.AngleMode != calc.AngleUser {
		t.Fatalf("the defaults are wrong: %+v", effective.Calculator)
	}
}

func TestCalculatorKeyReadsItsSettings(t *testing.T) {
	exam := calculatorExam(t, "mode=[scientific, neural], angle=rad, disabled=[softmax, ieee754]")
	c := exam.Tools.Calculator
	if !c.Enabled || !slices.Equal(c.Modes, []string{"scientific", "neural"}) || c.AngleMode != "rad" || !slices.Equal(c.DisabledFunctions, []string{"softmax", "ieee754"}) {
		t.Fatalf("the settings were not read: %+v", c)
	}
	// Single values work without brackets.
	exam = calculatorExam(t, "mode=programmer, angle=deg, disabled=factorial")
	c = exam.Tools.Calculator
	if !slices.Equal(c.Modes, []string{"programmer"}) || c.AngleMode != "deg" || !slices.Equal(c.DisabledFunctions, []string{"factorial"}) {
		t.Fatalf("single values were not read: %+v", c)
	}
	// Spaces around the settings do not matter, and neither does a trailing comma.
	exam = calculatorExam(t, " mode = [ scientific , programmer ] ,  angle = user ,")
	if !slices.Equal(exam.Tools.Calculator.Modes, []string{"scientific", "programmer"}) {
		t.Fatalf("a loose style changed the modes: %+v", exam.Tools.Calculator)
	}
}

// The compiled exam document is the server's POST body; tools must survive the trip unchanged.
func TestCalculatorSurvivesTheJSONRoundTrip(t *testing.T) {
	exam := calculatorExam(t, "mode=[scientific, neural], angle=rad, disabled=[softmax]")
	encoded, err := json.Marshal(exam)
	if err != nil {
		t.Fatal(err)
	}
	var back Exam
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if back.Tools == nil || back.Tools.Version != 1 || back.Tools.Calculator == nil || !back.Tools.Calculator.Enabled {
		t.Fatalf("tools did not survive the round trip: %s", encoded)
	}
	if !slices.Equal(back.Tools.Calculator.Modes, []string{"scientific", "neural"}) || back.Tools.Calculator.AngleMode != "rad" {
		t.Fatalf("the round trip changed the values: %+v", back.Tools.Calculator)
	}
	// An exam without the key sends no tools object at all.
	exam = calculatorExam(t, "")
	exam.Tools = nil
	encoded, _ = json.Marshal(exam)
	if strings.Contains(string(encoded), "\"tools\"") {
		t.Fatalf("an exam without tools still carries the key: %s", encoded)
	}
}

func TestCalculatorProblemsNameTheirLines(t *testing.T) {
	for name, test := range map[string]struct{ source, want string }{
		"unknown mode":        {calculatorSource("mode=[quaternion]"), `line 4: calculator: mode=[…] names no mode "quaternion"`},
		"empty mode list":     {calculatorSource("mode=[]"), "line 4: calculator: the mode list is empty"},
		"unclosed mode list":  {calculatorSource("mode=[scientific"), "line 4: calculator: mode=[scientific is not a list"},
		"unknown disabled":    {calculatorSource("disabled=[solve]"), `line 4: calculator: disabled=[…] names nothing in the registry: "solve"`},
		"empty disabled list": {calculatorSource("disabled=[]"), "line 4: calculator: the disabled list is empty"},
		"unknown setting":     {calculatorSource("wx=3"), `line 4: calculator: unknown setting "wx"`},
		"not a setting":       {calculatorSource("mode=[scientific] angle=rad"), "line 4: calculator: mode=[scientific] angle=rad is not a list"},
		"angle twice":         {calculatorSource("angle=rad, angle=deg"), "line 4: calculator: angle is set twice"},
		"bad angle":           {calculatorSource("angle=gradians"), "line 4: calculator: angle must be deg, rad or user"},
		"calculator twice":    {calculatorSource("\ncalculator:"), "line 5: calculator is set twice"},
		"stray angle key":     {"---\ntitle: Quiz\nduration: 30m\ncalculator: mode=[scientific]\ncalculator_angle: rad\n---\n## A\n{{x}}\n", `line 5: unknown front matter key "calculator_angle"`},
		"stray disabled key":  {"---\ntitle: Quiz\nduration: 30m\ncalculator: mode=[scientific]\ncalculator_disabled: softmax\n---\n## A\n{{x}}\n", `line 5: unknown front matter key "calculator_disabled"`},
		"stray log key":       {"---\ntitle: Quiz\nduration: 30m\ncalculator: mode=[scientific]\ncalculator_log: false\n---\n## A\n{{x}}\n", `line 5: unknown front matter key "calculator_log"`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile([]byte(test.source))
			var problems Problems
			if !errors.As(err, &problems) {
				t.Fatalf("expected problems, got %v", err)
			}
			if !strings.Contains(problems.Error(), test.want) {
				t.Fatalf("got %q, want a problem containing %q", problems.Error(), test.want)
			}
		})
	}
}

// A disabled name that exists is accepted even when its mode is off (D16), and the config stays valid.
func TestCalculatorAcceptsDisabledNamesOfOffModes(t *testing.T) {
	exam := calculatorExam(t, "mode=[scientific], disabled=[matmul, nand]")
	if exam.Tools == nil || exam.Tools.Calculator.DisabledFunctions == nil {
		t.Fatalf("the disabled list was not kept: %+v", exam.Tools)
	}
}

// The sample with a calculator must keep compiling, so a format change cannot slip past it.
func TestCalculatorSampleCompiles(t *testing.T) {
	source, err := os.ReadFile("testdata/calculator.md")
	if err != nil {
		t.Fatal(err)
	}
	exam, err := Compile(source)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if exam.Tools == nil || !exam.Tools.On() || !slices.Equal(exam.Tools.Calculator.Modes, []string{"scientific", "programmer"}) {
		t.Fatalf("the sample lost its calculator: %+v", exam.Tools)
	}
	if exam.Tools.Calculator.AngleMode != "rad" || !slices.Equal(exam.Tools.Calculator.DisabledFunctions, []string{"softmax"}) {
		t.Fatalf("the sample lost its settings: %+v", exam.Tools.Calculator)
	}
}

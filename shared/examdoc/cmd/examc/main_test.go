package main

import (
	"testing"

	"exam-platform/shared/calc"
)

// The summary names the angle setting only when scientific mode is on: no other mode has trigonometric functions,
// so for them the setting has no effect.
func TestCalculatorSummary(t *testing.T) {
	on := func(modes []string, angle string, disabled ...string) *calc.Tools {
		return &calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: true, Modes: modes, AngleMode: angle, DisabledFunctions: disabled}}
	}
	for _, test := range []struct {
		tools *calc.Tools
		want  string
	}{
		{nil, ""},
		{&calc.Tools{Version: 1, Calculator: &calc.Calculator{Enabled: false}}, ""},
		{on(nil, ""), "calculator: scientific · selectable angle"},
		{on([]string{calc.ModeScientific, calc.ModeNeural}, calc.AngleRadians, "softmax", "matmul"), "calculator: scientific, neural · radians · 2 functions disabled"},
		{on([]string{calc.ModeScientific}, calc.AngleDegrees), "calculator: scientific · degrees"},
		{on([]string{calc.ModeProgrammer}, ""), "calculator: programmer"},
		{on([]string{calc.ModeProgrammer}, calc.AngleRadians, "ieee754"), "calculator: programmer · 1 function disabled"},
		{on([]string{calc.ModeNeural}, calc.AngleDegrees), "calculator: neural"},
	} {
		if got := calculatorSummary(test.tools); got != test.want {
			t.Errorf("summary = %q, want %q", got, test.want)
		}
	}
}

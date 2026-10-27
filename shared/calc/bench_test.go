package calc

import (
	"strings"
	"testing"
	"time"
)

// The performance targets of the plan's Phase 8: a 200-character expression evaluates in under 50 ms, and so
// does an 8 by 8 matrix multiplication. The benchmarks measure them; the test checks the targets with a wide
// margin, so a slow machine does not fail it but a quadratic mistake would.

// longScientific is 200 characters of nested calls and operators, the longest input the engine accepts.
var longScientific = func() string {
	unit := "sin(cos(1.5))*sqrt(2)+ln(3)-"
	return pad(strings.Repeat(unit, maxInputLength/len(unit)))
}()

// longProgrammer is 200 characters of programmer operators.
var longProgrammer = func() string {
	unit := "(0xFF & 0x0F) << 2 ^ "
	return pad(strings.Repeat(unit, maxInputLength/len(unit)))
}()

// pad completes an input that ends with an operator to exactly 200 characters with small terms: 1+1+1...
func pad(input string) string {
	input += "1"
	for len(input)+2 <= maxInputLength {
		input += "+1"
	}
	return input + strings.Repeat(" ", maxInputLength-len(input))
}

// square8 is an 8 by 8 matrix, the largest one, as the ans of a matmul(ans, ans).
var square8 = func() Quantity {
	q := Quantity{Shape: ShapeMatrix, Rows: maxMatrix, Cols: maxMatrix, Data: make([]float64, maxMatrix*maxMatrix)}
	for i := range q.Data {
		q.Data[i] = float64(i%7) - 3.5
	}
	return q
}()

func BenchmarkEvaluate200Characters(b *testing.B) {
	opts := Options{AngleMode: AngleRadians, Allowed: allowedScientific()}
	for b.Loop() {
		if _, err := Evaluate(longScientific, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvaluateProgrammer200Characters(b *testing.B) {
	opts := IntOptions{Word: 32, Signed: true, Base: 10}
	for b.Loop() {
		if _, err := EvaluateProgrammer(longProgrammer, opts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMatmul8x8(b *testing.B) {
	opts := neuralOptions(square8)
	for b.Loop() {
		if _, err := Evaluate("matmul(ans, ans)", opts); err != nil {
			b.Fatal(err)
		}
	}
}

func TestEvaluationMeetsThePerformanceTargets(t *testing.T) {
	if len(longScientific) != maxInputLength || len(longProgrammer) != maxInputLength {
		t.Fatalf("the inputs are %d and %d characters", len(longScientific), len(longProgrammer))
	}
	for name, run := range map[string]func() error{
		"scientific": func() error {
			_, err := Evaluate(longScientific, Options{AngleMode: AngleRadians, Allowed: allowedScientific()})
			return err
		},
		"programmer": func() error {
			_, err := EvaluateProgrammer(longProgrammer, IntOptions{Word: 32, Signed: true, Base: 10})
			return err
		},
		"matmul": func() error { _, err := Evaluate("matmul(ans, ans)", neuralOptions(square8)); return err },
	} {
		start := time.Now()
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
			t.Errorf("%s took %v, the target is 50 ms", name, elapsed)
		}
	}
}

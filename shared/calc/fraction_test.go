package calc

import "testing"

// The base-conversion test vectors of the plan (Phases 5 and 6A): exact from the value's rational, with the
// 32-digit cap and the three ASCII dots (D6), uppercase hex digits, and the shared input read through the
// programmer evaluator (D21).
func TestFractionConversion(t *testing.T) {
	opts := optsInt(32, true, 10)
	for input, want := range map[string]FractionResult{
		"10.625": {Binary: "1010.101", Octal: "12.5", Hex: "A.A", Exact: true},
		"0.1": {Binary: "0.00011001100110011001100110011001...", Octal: "0.06314631463146314631463146314631...",
			Hex: "0.19999999999999999999999999999999..."},
		"1.0 / 3": {Binary: "0.01010101010101010101010101010101...", Octal: "0.25252525252525252525252525252525...",
			Hex: "0.55555555555555555555555555555555..."},
		"7":      {Binary: "111", Octal: "7", Hex: "7", Exact: true},
		"-0.5":   {Binary: "-0.1", Octal: "-0.4", Hex: "-0.8", Exact: true},
		"1 / 3":  {Binary: "0", Octal: "0", Hex: "0", Exact: true}, // integer division (D20)
		"255.75": {Binary: "11111111.11", Octal: "377.6", Hex: "FF.C", Exact: true},
		"-0.0":   {Binary: "-0", Octal: "-0", Hex: "-0", Exact: true},
	} {
		result, err := Fraction(input, opts)
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if result != want {
			t.Fatalf("%q = %+v, want %+v", input, result, want)
		}
	}
	// An integer is read through the signed/unsigned toggle.
	if result, err := Fraction("0xFF", optsInt(8, true, 10)); err != nil || result.Binary != "-1" {
		t.Fatalf("signed 8-bit 0xFF = %+v, %v", result, err)
	}
	if result, err := Fraction("0xFF", optsInt(8, false, 10)); err != nil || result.Binary != "11111111" {
		t.Fatalf("unsigned 8-bit 0xFF = %+v, %v", result, err)
	}
	for bad, code := range map[string]string{
		"": CodeSyntax, "1e3": CodeSyntax, "1.2.3": CodeSyntax, ".": CodeSyntax, "1.": CodeSyntax, "1,5": CodeSyntax,
		"1.5 & 1": CodeNotInteger, "abc": CodeUnknownIdent,
	} {
		if _, err := Fraction(bad, opts); err == nil || err.(*Error).Code != code {
			t.Fatalf("%q = %v, want %s", bad, err, code)
		}
	}
}

// FractionDigits says truncated only when a remainder is left after the digit cap.
func TestFractionDigitsTruncation(t *testing.T) {
	if digits, truncated := FractionDigits(rat("0.5"), 2, 1); digits != "0.1" || truncated {
		t.Fatalf("0.5 with one digit = %q, %v", digits, truncated)
	}
	if digits, truncated := FractionDigits(rat("0.75"), 2, 1); digits != "0.1" || !truncated {
		t.Fatalf("0.75 with one digit = %q, %v", digits, truncated)
	}
}

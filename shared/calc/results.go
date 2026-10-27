package calc

// The Wails-facing results of the calculator, shared by every host of the calculator component (the student
// application's service and the proctor dashboard's preview): a failed evaluation is a result with a code,
// never a Go error, so the frontend maps codes in one place and never parses messages.

// CalcResult is what a scientific evaluation answers. A matrix also comes as Rows, its formatted elements row by
// row, so the panel can align the columns; Display is always the one-line form the log records.
type CalcResult struct {
	Ok      bool       `json:"ok"`
	Display string     `json:"display,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`
	Code    string     `json:"code,omitempty"`
}

// ScientificResult is a successful scientific evaluation as the frontend reads it.
func ScientificResult(result Result) CalcResult {
	return CalcResult{Ok: true, Display: result.Display, Rows: result.Value.Cells()}
}

// CalcIntResult is what a programmer evaluation answers: for an integer, every reading of the N-bit value as
// strings (a 64-bit unsigned value exceeds the JS safe integers, so the frontend never receives raw numbers) and
// the flags; for a real (D20), only its display, in Value; on failure the code.
type CalcIntResult struct {
	Ok    bool   `json:"ok"`
	Real  bool   `json:"real,omitempty"`
	Value string `json:"value,omitempty"` // a real's display

	Signed   string `json:"signed,omitempty"`
	Unsigned string `json:"unsigned,omitempty"`
	Hex      string `json:"hex,omitempty"`
	Octal    string `json:"octal,omitempty"`
	Binary   string `json:"binary,omitempty"`
	Bits     string `json:"bits,omitempty"` // the raw value as a decimal string, for exact frontend handling
	Carry    bool   `json:"carry,omitempty"`
	Overflow bool   `json:"overflow,omitempty"`
	Code     string `json:"code,omitempty"`
}

// ProgrammerResult is a successful programmer evaluation as the frontend reads it, and the display its log
// records: a real's display, or an integer's unsigned reading at the word size.
func ProgrammerResult(value Value, word int) (CalcIntResult, string) {
	if value.IsReal() {
		display := FormatReal(value.Real, value.NegativeZero)
		return CalcIntResult{Ok: true, Real: true, Value: display}, display
	}
	display := FormatInt(value.Bits, effectiveWord(word))
	return CalcIntResult{Ok: true, Signed: display.Signed, Unsigned: display.Unsigned, Hex: display.Hex, Octal: display.Octal,
		Binary: display.Binary, Bits: display.Unsigned, Carry: value.Carry, Overflow: value.Overflow}, display.Unsigned
}

// CalcFractionResult is one value's exact readings in the programmer panel's Radix Point tab (D6).
type CalcFractionResult struct {
	Ok     bool   `json:"ok"`
	Binary string `json:"binary,omitempty"`
	Octal  string `json:"octal,omitempty"`
	Hex    string `json:"hex,omitempty"`
	Code   string `json:"code,omitempty"`
}

// CalcIEEEView is what an inspection answers the frontend: the reading, or the code on failure. A failed
// inspection is a result, never a Go error, like every evaluation.
type CalcIEEEView struct {
	Ok   bool   `json:"ok"`
	Code string `json:"code,omitempty"`
	IEEEView
}

// ResolveAngle decides the angle mode of an evaluation: a configured mode wins; only the user-selectable mode
// (D3) listens to what the frontend sends, and anything unexpected reads as radians.
func ResolveAngle(configured, requested string) string {
	if configured != "" && configured != AngleUser {
		return configured
	}
	if requested == AngleDegrees {
		return AngleDegrees
	}
	return AngleRadians
}

// InputBase reads the programmer panel's base selector: bin, oct, dec or hex.
func InputBase(inputBase string) (int, bool) {
	switch inputBase {
	case "bin":
		return 2, true
	case "oct":
		return 8, true
	case "dec":
		return 10, true
	case "hex":
		return 16, true
	}
	return 0, false
}

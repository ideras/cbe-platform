package calc

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// encode and decode are the tests' entries to the two directions of the inspector (D21), with the default
// panel settings: 32 bits, signed, DEC.
func encode(input, format string) (IEEEView, error) {
	return Inspect(input, format, DirectionEncode, optsInt(32, true, 10))
}

func decode(input, format string) (IEEEView, error) {
	return Inspect(input, format, DirectionDecode, optsInt(32, true, 10))
}

// The test vectors of the plan (Phases 6 and 6A).
func TestIEEETestVectors(t *testing.T) {
	for name, test := range map[string]struct {
		direction, input, format, hex, class, value string
	}{
		"binary16 one":         {DirectionEncode, "1.0", "binary16", "0x3C00", "normal", "1"},
		"binary16 max":         {DirectionEncode, "65504", "binary16", "0x7BFF", "normal", "65504"},
		"smallest subnormal":   {DirectionEncode, "0.000000059604645", "binary16", "0x0001", "subnormal", "0.00000005960464477539063"},
		"2^-24 as bits":        {DirectionDecode, "0x0001", "binary16", "0x0001", "subnormal", "0.00000005960464477539063"},
		"binary16 of 0.1":      {DirectionEncode, "0.1", "binary16", "0x2E66", "normal", "0.0999755859375"},
		"bfloat16 one":         {DirectionEncode, "1.0", "bfloat16", "0x3F80", "normal", "1"},
		"binary64 of 0.1":      {DirectionEncode, "0.1", "binary64", "0x3FB999999999999A", "normal", "0.1"},
		"negative zero":        {DirectionEncode, "-0.0", "binary32", "0x80000000", "zero", "-0"},
		"the integer -0":       {DirectionEncode, "-0", "binary32", "0x00000000", "zero", "0"},
		"infinity":             {DirectionEncode, "70000.0", "binary16", "0x7C00", "infinity", "+Inf"},
		"a nan pattern":        {DirectionDecode, "0x7E00", "binary16", "0x7E00", "nan", "NaN"},
		"subnormal bits":       {DirectionDecode, "0x03FF", "binary16", "0x03FF", "subnormal", "0.00006097555160522461"},
		"10.625 binary32":      {DirectionEncode, "10.625", "binary32", "0x412A0000", "normal", "10.625"},
		"10.625 binary16":      {DirectionEncode, "10.625", "binary16", "0x4950", "normal", "10.625"},
		"10.625 bfloat16":      {DirectionEncode, "10.625", "bfloat16", "0x412A", "normal", "10.625"},
		"10.625 binary64":      {DirectionEncode, "10.625", "binary64", "0x4025400000000000", "normal", "10.625"},
		"0x1e3 is a number":    {DirectionEncode, "0x1e3", "binary16", "0x5F8C", "normal", "483"},
		"an expression":        {DirectionEncode, "1.0 / 4 + 10", "binary16", "0x4920", "normal", "10.25"},
		"decode binary32":      {DirectionDecode, "0x412A0000", "binary32", "0x412A0000", "normal", "10.625"},
		"decode an expression": {DirectionDecode, "0x3C00 | 0x8000", "binary16", "0xBC00", "normal", "-1"},
		"decode binary digits": {DirectionDecode, "0b0011110000000000", "binary16", "0x3C00", "normal", "1"},
		"decode octal digits":  {DirectionDecode, "0o36000", "binary16", "0x3C00", "normal", "1"},
		"decode -1":            {DirectionDecode, "-1", "binary16", "0xFFFF", "nan", "NaN"},
		"decode a short one":   {DirectionDecode, "0x3C0", "binary16", "0x03C0", "subnormal", "0.000057220458984375"},
	} {
		t.Run(name, func(t *testing.T) {
			view, err := Inspect(test.input, test.format, test.direction, optsInt(32, true, 10))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if view.Bits != test.hex || view.Class != test.class || view.Value != test.value {
				t.Fatalf("%s = %+v, want bits %s class %s value %s", name, view, test.hex, test.class, test.value)
			}
			if test.class == "infinity" && !view.Rounded {
				t.Fatalf("%s: an overflow-rounded input should say so: %+v", name, view)
			}
		})
	}
	// Rounding at the subnormal grid: below half the smallest subnormal rounds to zero; between subnormals,
	// to the nearest one; the views say the input had more precision than the format.
	view, err := encode("0.00000002", "binary16") // below half of 2^-24
	if err != nil || view.Class != "zero" || !view.Rounded {
		t.Fatalf("a value below half the smallest subnormal = %+v, %v", view, err)
	}
	view, _ = encode("0.00000009", "binary16") // between the two smallest subnormals
	if view.Bits != "0x0002" || !view.Rounded {
		t.Fatalf("a value between subnormals = %+v", view)
	}
	// 65520 sits exactly halfway between the largest normal and overflow: ties to even is infinity.
	view, _ = encode("65520", "binary16")
	if view.Bits != "0x7C00" || view.Class != "infinity" || !view.Rounded {
		t.Fatalf("65520 = %+v, want 0x7C00 by the tie to even", view)
	}
	if view, _ = encode("10.625", "binary16"); view.Rounded {
		t.Fatalf("an exact value says it was rounded: %+v", view)
	}
	// Encode reads an integer through the signed/unsigned toggle (owner, 2026-10-24).
	signed, _ := Inspect("0xFF", "binary16", DirectionEncode, optsInt(8, true, 10))
	unsigned, _ := Inspect("0xFF", "binary16", DirectionEncode, optsInt(8, false, 10))
	if signed.Bits != "0xBC00" || unsigned.Bits != "0x5BF8" {
		t.Fatalf("8-bit 0xFF encodes as %s signed and %s unsigned, want 0xBC00 and 0x5BF8", signed.Bits, unsigned.Bits)
	}
	// Decode works at the format's width, whatever the word size.
	if view, err := Inspect("0x412A0000", "binary32", DirectionDecode, optsInt(8, true, 10)); err != nil || view.Value != "10.625" {
		t.Fatalf("decoding binary32 at an 8-bit word size = %+v, %v", view, err)
	}
}

func TestIEEERefusals(t *testing.T) {
	for _, test := range []struct{ direction, input, format, code string }{
		{DirectionDecode, "0x412A0000", "binary16", CodeBitWidth},
		{DirectionDecode, "0x10000", "bfloat16", CodeBitWidth},
		{DirectionDecode, "0x1FFFFFFFF", "binary32", CodeBitWidth},
		{DirectionDecode, "1.5", "binary16", CodeNotInteger},
		{DirectionDecode, "2.0", "binary16", CodeNotInteger},
		{DirectionDecode, "0x3C0G", "binary16", CodeSyntax},
		{DirectionEncode, "0x3C00", "binary17", CodeSyntax}, // no such format
		{"sideways", "1.0", "binary16", CodeSyntax},         // no such direction
		{DirectionEncode, "", "binary16", CodeSyntax},
		{DirectionEncode, "1e", "binary32", CodeSyntax},
		{DirectionEncode, "1e-7", "binary32", CodeSyntax}, // no exponent (D19)
		{DirectionEncode, "1e40", "binary32", CodeSyntax},
		{DirectionEncode, "1.2.3", "binary32", CodeSyntax},
		{DirectionEncode, "1.5 & 1", "binary32", CodeNotInteger},
		{DirectionEncode, "1.0 / 0", "binary32", CodeDivisionByZero},
	} {
		if view, err := Inspect(test.input, test.format, test.direction, optsInt(32, true, 10)); err == nil {
			t.Fatalf("%s %q in %s = %+v, want a refusal", test.direction, test.input, test.format, view)
		} else if code := err.(*Error).Code; code != test.code {
			t.Fatalf("%s %q in %s = %s, want %s", test.direction, test.input, test.format, code, test.code)
		}
	}
}

// asReal writes a decimal as a real literal: an integer decimal gains ".0", so it is neither fitted to the word
// size nor read through the toggle.
func asReal(decimal string) string {
	if strings.Contains(decimal, ".") {
		return decimal
	}
	return decimal + ".0"
}

// Exhaustive: all 65,536 binary16 patterns. Each non-NaN pattern decodes and re-encodes to the same bits; each
// NaN pattern re-encodes to some NaN (plan Phase 6).
func TestIEEEBinary16Exhaustive(t *testing.T) {
	for bits := 0; bits < 1<<16; bits++ {
		view, err := decode("0x"+hex4(uint64(bits)), "binary16")
		if err != nil {
			t.Fatalf("0x%04X: %v", bits, err)
		}
		if view.Class == "nan" {
			again, err := decode(view.Bits, "binary16")
			if err != nil || again.Class != "nan" {
				t.Fatalf("NaN 0x%04X re-encoded to %s, %v", bits, view.Bits, err)
			}
			continue
		}
		if view.Class == "infinity" {
			// Infinity has no decimal to feed back; the bit pattern is its own round trip.
			continue
		}
		again, err := encode(asReal(view.Value), "binary16")
		if err != nil {
			t.Fatalf("0x%04X: %s does not read back: %v", bits, view.Value, err)
		}
		// The view's value is the shortest decimal that reads back as the same float64, not always the bits'
		// exact value, so the re-encoding may say it rounded; the bits must match.
		if again.Bits != view.Bits {
			t.Fatalf("0x%04X (%s) re-encoded to %s", bits, view.Bits, again.Bits)
		}
	}
}

// binary32 and binary64 decode every pattern, and encode decimals exactly as Go's own conversions do, on 10,000
// random inputs (plan Phase 6).
func TestIEEERandomAgreesWithGo(t *testing.T) {
	generator := rand.New(rand.NewSource(2035))
	for i := 0; i < 10000; i++ {
		f64 := math.Float64frombits(generator.Uint64())
		if math.IsNaN(f64) || math.IsInf(f64, 0) {
			continue // a NaN's exact payload is not a decimal number's business
		}
		view, err := decode("0x"+hex16(math.Float64bits(f64)), "binary64")
		if err != nil || view.Bits != "0x"+hex16(math.Float64bits(f64)) {
			t.Fatalf("binary64 of %v: %v", f64, err)
		}
		f32 := float32(f64)
		view, err = decode("0x"+hex8(uint64(math.Float32bits(f32))), "binary32")
		if err != nil || view.Bits != "0x"+hex8(uint64(math.Float32bits(f32))) {
			t.Fatalf("binary32 of %v: %v", f32, err)
		}
		// A 20-digit decimal, more digits than binary64 holds, written out exactly: Encode rounds it once, as
		// strconv's correctly rounded parser does, for binary64 and for binary32.
		mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(f64, 'e', 19, 64), "e")
		power, _ := strconv.Atoi(exponent)
		if power < -40 || power > 40 {
			continue // written out in full, the decimal would pass the 200-character limit
		}
		exact, _ := new(big.Rat).SetString(mantissa + "e" + exponent)
		text := asReal(exact.FloatString(max(0, 19-power)))
		if view, err = encode(text, "binary64"); err != nil || view.Bits != "0x"+hex16(math.Float64bits(mustParse(text))) {
			t.Fatalf("binary64 of %s = %+v, %v", text, view, err)
		}
		if view, err = encode(text, "binary32"); err != nil || view.Bits != "0x"+hex8(uint64(math.Float32bits(mustParse32(text)))) {
			t.Fatalf("binary32 of %s = %+v, %v", text, view, err)
		}
	}
}

// mustParse and mustParse32 are strconv's correctly rounded reading; out of range, it answers ±Inf with an error,
// and ±Inf is the expected encoding.
func mustParse(text string) float64 {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		panic(err)
	}
	return value
}

func mustParse32(text string) float32 {
	value, err := strconv.ParseFloat(text, 32)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		panic(err)
	}
	return float32(value)
}

// bfloat16 is rounded once, so for every input exact in binary32 it equals the upper 16 bits of the binary32
// encoding with the rounding carry applied (plan Phase 6).
func TestIEEEBFloat16AgreesWithBinary32(t *testing.T) {
	generator := rand.New(rand.NewSource(2035))
	for i := 0; i < 10000; i++ {
		bits32 := uint64(generator.Uint32()) // any 32-bit pattern
		if bits32>>23&0xFF == 0xFF {
			continue // NaN and infinity have no decimal to encode
		}
		value := float64(math.Float32frombits(uint32(bits32)))
		view, err := encode(asReal(shortestOf(value)), "bfloat16")
		if err != nil {
			t.Fatalf("%v: %v", value, err)
		}
		// The property is round-to-nearest-even, not truncation: the top 16 bits with the rounding carry of
		// the dropped half applied.
		want := bits32 >> 16
		if dropped := bits32 & 0xFFFF; dropped > 0x8000 || dropped == 0x8000 && want&1 == 1 {
			want++
		}
		if view.Bits != "0x"+hex4(want) {
			t.Fatalf("bfloat16 of %v (0x%08X) = %s, want 0x%04X", value, bits32, view.Bits, want)
		}
	}
}

func hex4(bits uint64) string  { return hexOf(bits, 4) }
func hex8(bits uint64) string  { return hexOf(bits, 8) }
func hex16(bits uint64) string { return hexOf(bits, 16) }

func hexOf(bits uint64, digits int) string {
	const hexDigits = "0123456789ABCDEF"
	out := make([]byte, digits)
	for i := digits - 1; i >= 0; i-- {
		out[i] = hexDigits[bits&0xF]
		bits >>= 4
	}
	return string(out)
}

// shortestOf is the shortest decimal that reads back as the value; the input goes through a decimal, like the
// inspector's real input does.
func shortestOf(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

// The view's JSON keys are the frontend's contract: lowercase snake_case, the shape the component's template
// reads. The fake stand-ins follow it, so only this test catches a missing tag.
func TestIEEEViewJSONKeys(t *testing.T) {
	view, err := encode("1.0", "binary16")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(CalcIEEEView{Ok: true, IEEEView: view})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`"format"`, `"bits"`, `"sign"`, `"exponent_field"`, `"unbiased_exponent"`, `"mantissa_field"`, `"class"`, `"value"`, `"ok":true`}
	for _, key := range want {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("the view's JSON lacks %s: %s", key, encoded)
		}
	}
	if strings.Contains(string(encoded), `"Bits"`) || strings.Contains(string(encoded), `"ExponentField"`) {
		t.Fatalf("the view's JSON leaks Go field names: %s", encoded)
	}
}

package calc

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// The IEEE 754 inspector's engine (plan Phases 6 and 6A, D13, D21): a number's bit-level encoding in binary16,
// bfloat16, binary32 and binary64. binary32 and binary64 use Go's own rounding (big.Float's Float32 and
// Float64); binary16 and bfloat16 share one round-to-nearest-even encoder, parameterized by the exponent and
// mantissa widths, and one decoder serves all four.
//
// Encode reads the shared input's value from its exact rational through a 256-bit big.Float, then rounds once
// to the format. The 256-bit step is a rounding too, so a double rounding remains possible, but only for a value
// within a relative 2^-256 of a halfway point between two values of the format without being on it (Appendix B).

// IEEEFormat describes one format: its name, total bits, exponent width and mantissa width.
type IEEEFormat struct {
	Name     string
	Bits     int
	Exponent int
	Mantissa int
}

// IEEEFormats are the four formats of the inspector, keyed by the name the config and the UI use.
var IEEEFormats = map[string]IEEEFormat{
	"binary16": {Name: "binary16", Bits: 16, Exponent: 5, Mantissa: 10},
	"bfloat16": {Name: "bfloat16", Bits: 16, Exponent: 8, Mantissa: 7},
	"binary32": {Name: "binary32", Bits: 32, Exponent: 8, Mantissa: 23},
	"binary64": {Name: "binary64", Bits: 64, Exponent: 11, Mantissa: 52},
}

// IEEEView is one inspection's reading: the fields, the classification, the bits as hex, and the exact value
// of the bits as a decimal string. Rounded says the decimal input had more precision than the format, so what
// is shown is the rounded value.
type IEEEView struct {
	Format           string `json:"format"`
	Bits             string `json:"bits"` // 0x…
	Sign             string `json:"sign"`
	ExponentField    string `json:"exponent_field"`
	UnbiasedExponent string `json:"unbiased_exponent"`
	MantissaField    string `json:"mantissa_field"`
	Class            string `json:"class"` // zero, subnormal, normal, infinity, nan
	Value            string `json:"value"`
	Rounded          bool   `json:"rounded,omitempty"`
}

// The inspector's two directions (D21): Encode evaluates the input and encodes its value, Decode evaluates an
// integer expression and decodes its bits.
const (
	DirectionEncode = "encode"
	DirectionDecode = "decode"
)

// Inspect reads the shared input in one of the four formats. Encode evaluates it as a programmer expression and
// encodes the value: a real exactly, an integer through the signed/unsigned toggle, so 0x1e3 is the number 483.
// Decode evaluates it as an integer expression at the format's width, whatever the word size says, and decodes
// the bits: an integer literal wider than the format is BIT_WIDTH, a real result NOT_INTEGER.
func Inspect(input string, formatName string, direction string, opts IntOptions) (IEEEView, error) {
	format, ok := IEEEFormats[formatName]
	if !ok {
		return IEEEView{}, &Error{Code: CodeSyntax}
	}
	switch direction {
	case DirectionEncode:
		value, err := EvaluateProgrammer(input, opts)
		if err != nil {
			return IEEEView{}, err
		}
		rational := value.Rational(effectiveWord(opts.Word), opts.Signed)
		view, exact := read(encodeRational(rational, value.NegativeZero, format), format)
		view.Rounded = math.IsInf(exact, 0) || math.IsNaN(exact) || new(big.Rat).SetFloat64(exact).Cmp(rational) != 0
		return view, nil
	case DirectionDecode:
		opts.Word = format.Bits
		value, err := evaluateProgrammer(input, opts, CodeBitWidth)
		if err != nil {
			return IEEEView{}, err
		}
		if value.IsReal() {
			return IEEEView{}, &Error{Code: CodeNotInteger}
		}
		view, _ := read(value.Bits, format)
		return view, nil
	}
	return IEEEView{}, &Error{Code: CodeSyntax}
}

// encodeRational rounds an exact value into the format with round-to-nearest-even, through a 256-bit big.Float.
// A negative zero stays negative.
func encodeRational(r *big.Rat, negativeZero bool, format IEEEFormat) uint64 {
	value := new(big.Float).SetPrec(256).SetMode(big.ToNearestEven).SetRat(r)
	if r.Sign() == 0 && negativeZero {
		value.Neg(value)
	}
	switch format.Name {
	case "binary32":
		f, _ := value.Float32()
		return uint64(math.Float32bits(f))
	case "binary64":
		f, _ := value.Float64()
		return math.Float64bits(f)
	}
	return encodeIEEE(value, format)
}

// read turns a bit pattern into the view: fields, classification and exact value. It also answers the value as a
// float64, which holds every value of the four formats exactly.
func read(bits uint64, format IEEEFormat) (IEEEView, float64) {
	exponentBits := uint(format.Exponent)
	mantissaBits := uint(format.Mantissa)
	sign := bits >> (exponentBits + mantissaBits) & 1
	exponent := bits >> mantissaBits & ((1 << exponentBits) - 1)
	mantissa := bits & ((1 << mantissaBits) - 1)
	bias := int64(1<<(exponentBits-1)) - 1
	signText := "0"
	value := 0.0
	class := "zero"
	unbiased := "0"
	switch {
	case exponent == (1<<exponentBits)-1:
		if mantissa != 0 {
			class = "nan"
			value = math.NaN()
		} else {
			class = "infinity"
			if sign == 0 {
				value = math.Inf(1)
			} else {
				value = math.Inf(-1)
			}
		}
	case exponent == 0 && mantissa == 0:
		class = "zero"
		value = 0
		if sign == 1 {
			value = math.Copysign(0, -1)
		}
	case exponent == 0:
		class = "subnormal"
		value = math.Ldexp(float64(mantissa), int(1-bias)-int(mantissaBits))
		unbiased = formatInt64(1 - bias)
		if sign == 1 {
			value = -value
		}
	default:
		class = "normal"
		value = math.Ldexp(float64(1<<mantissaBits+mantissa), int(exponent)-int(bias)-int(mantissaBits))
		unbiased = formatInt64(int64(exponent) - bias)
		if sign == 1 {
			value = -value
		}
	}
	if sign == 1 {
		signText = "1"
	}
	width := format.Bits / 4
	digits := strconv.FormatUint(bits, 16)
	hex := "0x" + strings.ToUpper(strings.Repeat("0", width-len(digits))+digits)
	return IEEEView{Format: format.Name, Bits: hex, Sign: signText, ExponentField: strconv.FormatInt(int64(exponent), 10),
		UnbiasedExponent: unbiased, MantissaField: strconv.FormatInt(int64(mantissa), 10), Class: class, Value: classValueText(class, value, sign)}, value
}

func classValueText(class string, value float64, sign uint64) string {
	switch class {
	case "nan":
		return "NaN"
	case "infinity":
		if sign == 1 {
			return "-Inf"
		}
		return "+Inf"
	case "zero":
		if sign == 1 {
			return "-0"
		}
		return "0"
	}
	return shortestDecimal(value)
}

// shortestDecimal is the shortest decimal that reads back as the value.
func shortestDecimal(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

// encodeIEEE rounds an exact big.Float into the format with round-to-nearest-even: the rounding of the mantissa
// and of the subnormal field are both exact integer roundings. The value is finite; an overflow rounds into
// infinity.
func encodeIEEE(exact *big.Float, format IEEEFormat) uint64 {
	exponentBits := uint(format.Exponent)
	mantissaBits := uint(format.Mantissa)
	bias := int64(1<<(exponentBits-1)) - 1
	minNormal := int64(1) - bias
	exact = new(big.Float).Copy(exact)
	sign := uint64(0)
	if exact.Signbit() {
		sign = 1
		exact.Abs(exact)
	}
	if exact.Sign() == 0 {
		return sign << (exponentBits + mantissaBits)
	}
	mant := new(big.Float)
	exp2 := exact.MantExp(mant) // exact = mant × 2^exp2, mant ∈ [0.5, 1)
	unbiased := int64(exp2) - 1
	if unbiased < minNormal {
		// Subnormal: the value is field × 2^(minNormal - manBits), the field an integer rounded from the exact
		// scaled value. Fewer effective bits than the format's precision — the scaling does the rounding.
		// The scale is 2^(manBits - minNormal): the smallest subnormal's place value, times the mantissa's grid.
		scale := new(big.Float).SetMantExp(big.NewFloat(0.5), int(mantissaBits)-int(minNormal)+1)
		scaled := roundEven(new(big.Float).SetMode(big.ToNearestEven).Mul(exact, scale))
		if scaled.Cmp(new(big.Int).Lsh(big.NewInt(1), mantissaBits)) == 0 {
			// Rounded up into the smallest normal.
			return sign<<(exponentBits+mantissaBits) | uint64(1)<<mantissaBits
		}
		return sign<<(exponentBits+mantissaBits) | scaled.Uint64()
	}
	// Normal: round the mantissa (mant' ∈ [1,2) as manBits+1 significant bits), then carry into the exponent.
	scaled := roundEven(new(big.Float).SetMode(big.ToNearestEven).Mul(mant, pow2(mantissaBits+1)))
	limit := new(big.Int).Lsh(big.NewInt(1), mantissaBits+1)
	if scaled.Cmp(limit) == 0 {
		scaled.Rsh(scaled, 1)
		unbiased++
	}
	if unbiased > bias { // the exponent field would be all ones: the value rounded into infinity
		bits := uint64(1)<<exponentBits<<mantissaBits - 1<<mantissaBits
		return sign<<(exponentBits+mantissaBits) | bits
	}
	mantissaField := new(big.Int).Sub(scaled, new(big.Int).Lsh(big.NewInt(1), mantissaBits))
	return sign<<(exponentBits+mantissaBits) | uint64(unbiased+bias)<<mantissaBits | mantissaField.Uint64()
}

// pow2 is 2^n as a big.Float. SetMantExp(0.5, n+1) is 0.5 × 2^(n+1) = 2^n.
func pow2(n uint) *big.Float { return new(big.Float).SetMantExp(big.NewFloat(0.5), int(n)+1) }

// roundEven rounds a non-negative big.Float to the nearest integer, ties to even, exactly.
func roundEven(x *big.Float) *big.Int {
	integral, _ := x.Int(nil)
	fraction := new(big.Float).SetMode(big.ToNearestEven).Sub(x, new(big.Float).SetMode(big.ToNearestEven).SetInt(integral))
	switch fraction.Cmp(big.NewFloat(0.5)) {
	case -1:
		return integral
	case 1:
		return new(big.Int).Add(integral, big.NewInt(1))
	default:
		if integral.Bit(0) == 1 {
			return new(big.Int).Add(integral, big.NewInt(1))
		}
		return integral
	}
}

// formatInt64 formats a small field value.
func formatInt64(value int64) string { return strconv.FormatInt(value, 10) }

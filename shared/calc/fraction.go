package calc

import (
	"math/big"
	"strings"
)

// The Radix Point tab (plan Phases 5 and 6A): a value's binary, octal and hexadecimal readings, exactly, from its
// rational — never through a float64. The integer part is converted too (10.625 → 1010.101); the fraction is
// read digit by digit by repeatedly multiplying it by the base and taking the integer part, stopping after 32
// digits with three ASCII dots appended when the expansion goes on (D6).
const fractionDigits = 32

type FractionResult struct {
	Binary, Octal, Hex string
	Exact              bool // false when some base's expansion was cut at the digit cap
}

// Fraction evaluates the shared input as a programmer expression (D21) and converts its value: a real exactly,
// an integer through the signed/unsigned toggle, so 1.0 / 3 is 0.0101… and 8-bit 0xFF is -1 when signed.
func Fraction(input string, opts IntOptions) (FractionResult, error) {
	value, err := EvaluateProgrammer(input, opts)
	if err != nil {
		return FractionResult{}, err
	}
	rational := value.Rational(effectiveWord(opts.Word), opts.Signed)
	result := FractionResult{Exact: true}
	for _, base := range []struct {
		radix int
		name  *string
	}{{2, &result.Binary}, {8, &result.Octal}, {16, &result.Hex}} {
		digits, truncated := FractionDigits(rational, base.radix, fractionDigits)
		if truncated {
			digits += "..." // three ASCII dots (D6)
			result.Exact = false
		}
		if value.NegativeZero {
			digits = "-" + digits
		}
		*base.name = digits
	}
	return result, nil
}

// FractionDigits writes r in the base: the integer part by repeated division, the fraction by repeated
// multiplication, at most maxDigits fraction digits. Digits above 9 are uppercase, like the integer readout.
// truncated says a remainder is left after maxDigits digits; the caller appends the marker.
func FractionDigits(r *big.Rat, base int, maxDigits int) (digits string, truncated bool) {
	const digitSet = "0123456789ABCDEF"
	value := new(big.Rat).Abs(r)
	radix := big.NewInt(int64(base))
	integer := new(big.Int).Quo(value.Num(), value.Denom())
	rest := new(big.Rat).Sub(value, new(big.Rat).SetInt(integer))
	text := strings.ToUpper(integer.Text(base))
	var fraction []byte
	for len(fraction) < maxDigits && rest.Sign() != 0 {
		rest.Mul(rest, new(big.Rat).SetInt(radix))
		step := new(big.Int).Quo(rest.Num(), rest.Denom())
		fraction = append(fraction, digitSet[step.Int64()])
		rest.Sub(rest, new(big.Rat).SetInt(step))
	}
	if len(fraction) > 0 {
		text += "." + string(fraction)
	}
	if r.Sign() < 0 {
		text = "-" + text
	}
	return text, rest.Sign() != 0
}

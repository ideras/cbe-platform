// One module maps the engine's error codes to English: the Go engine returns codes, never prose (plan §3), and
// the same code must always mean the same words. The native errors of the Calc* methods (calculator unavailable,
// and from Phase 4 the full log queue) are strings on the JS side and are matched here by their Go text.
const messages = {
  SYNTAX: 'This is not a valid expression.',
  UNKNOWN_IDENT: 'This name has no value in this exam.',
  UNKNOWN_FUNCTION: 'This function is not available in this exam.',
  ARITY: 'This function takes a different number of arguments.',
  DIVISION_BY_ZERO: 'You cannot divide by zero.',
  DOMAIN: 'This value is outside the function’s domain.',
  OVERFLOW: 'The result is too large for the calculator.',
  TOO_LONG: 'The expression is too long — at most 200 characters.',
  TOO_DEEP: 'The expression is nested too deeply — at most 50 parentheses.',
  SHIFT_RANGE: 'A shift or rotation count is outside the word’s range.',
  NOT_INTEGER: 'Integer operators need whole numbers.',
  BIT_WIDTH: 'This bit pattern is wider than the format.',
  TYPE_MISMATCH: 'This operation got a value of the wrong kind.',
  DIM_MISMATCH: 'These values have incompatible shapes.',
  TOO_LARGE: 'This vector or matrix is too large — at most 64 elements, or 8 by 8.',
};

export function calcMessage(code) {
  return messages[code] ?? 'The calculation could not be evaluated.';
}

// The one-argument log() is the one arity failure worth explaining (D2): show how the three logarithms differ.
// In neural mode (Phase 7), a bce domain failure says what bce needs, and that it uses the natural log; a shape
// failure with * between brackets points to matmul, since * is elementwise.
export function calcHint(input, code) {
  if (code === 'ARITY' && /^\s*log\s*\(\s*[^,()]*\s*\)\s*$/.test(input)) {
    return 'Use log10(x) for base 10, ln(x) for natural log, or log(x, b) for base b.';
  }
  if (code === 'DOMAIN' && /\bbce\s*\(/.test(input)) {
    return 'bce(p, y) needs every p strictly between 0 and 1. It uses the natural log (ln).';
  }
  if (code === 'DIM_MISMATCH' && /\]\s*\*\s*\[/.test(input)) {
    return '* multiplies element by element. Use matmul(A, B) for the matrix product.';
  }
  return '';
}

// The programmer panel's one hint (Phase 6A): there is no exponent (D19), so an input that looks like one is
// explained, with the way around it — Decode, when the exam has the IEEE 754 tab.
export function programmerHint(input, code, { base = 'dec', decode = false } = {}) {
  if (code === 'SYNTAX' && base === 'dec' && /\d[eE][+-]?\d/.test(input)) {
    return decode ? 'Exponents are not accepted. Write the number in full or use Decode.' : 'Exponents are not accepted. Write the number in full.';
  }
  return '';
}

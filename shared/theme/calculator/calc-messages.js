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
  BAD_NUMBER: 'This number is not written correctly.',
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

// The programmer panel's hints. There is no input base (owner 2026-11-08): a plain number is decimal and the
// other bases take a prefix, so the hints explain a number written another way, with the way to write it. There
// is no exponent either (D19): an input that looks like one is explained, with the way around it — Decode, when
// the exam has the IEEE 754 tab. The Go lexer stops at the first bad word, and so does this scan.
const bases = {
  b: { name: 'binary', digits: '01', uses: 'only 0 and 1', example: '0b1010' },
  o: { name: 'octal', digits: '01234567', uses: '0 to 7', example: '0o17' },
  x: { name: 'hex', digits: '0123456789abcdef', uses: '0 to 9 and A to F', example: '0xFF' },
  d: { name: 'decimal', digits: '0123456789', uses: '0 to 9', example: '0d10' },
};
const separators = /^[^_](?:_?[^_])*$/; // every _ between two digits

const separatorHint = 'Use _ only between two digits, as in 0b1010_0101 or 1_000_000.';

// A word that starts with a digit and is no number (BAD_NUMBER). `fraction` is the word after a '.' that follows
// it, or null when there is no point.
function numberHint(word, fraction) {
  const prefixed = /^0([xXbBoOdD])(.*)$/.exec(word);
  if (prefixed) {
    const base = bases[prefixed[1].toLowerCase()];
    const digits = prefixed[2];
    const article = /^[aeiou]/.test(base.name) ? 'an' : 'a';
    if (!digits.replace(/_/g, '')) return `Write ${base.name} digits after 0${prefixed[1]}, as in ${base.example}.`;
    const bad = [...digits].find(c => c !== '_' && !base.digits.includes(c.toLowerCase()));
    if (bad) return `${bad} is not ${article} ${base.name} digit: ${base.name} uses ${base.uses}.`;
    if (fraction !== null && base.name !== 'decimal') return 'Only decimal numbers take a point, as in 10.625.';
    if (!separators.test(digits)) return separatorHint;
    return fraction === null ? '' : pointHint(fraction);
  }
  if (fraction !== null) {
    if (!/^[0-9_]+$/.test(word)) return 'Only decimal numbers take a point, as in 10.625.';
    return separators.test(word) ? pointHint(fraction) : separatorHint;
  }
  if (/^[01]+[bB]$/.test(word)) return `Write binary numbers with the 0b prefix: 0b${word.slice(0, -1)}.`;
  if (/^[0-7]+[oOqQ]$/.test(word)) return `Write octal numbers with the 0o prefix: 0o${word.slice(0, -1)}.`;
  if (/^[0-9][0-9a-fA-F]*[hH]$/.test(word)) return `Write hex numbers with the 0x prefix: 0x${word.slice(0, -1)}.`;
  if (/^[0-9a-fA-F]+$/.test(word) && /[a-fA-F]/.test(word)) return `Hex numbers need the 0x prefix: 0x${word}.`;
  if (/^0[0-9_]+$/.test(word)) {
    const digits = word.replace(/_/g, '');
    const decimal = digits.replace(/^0+(?=\d)/, '');
    return /^[0-7]+$/.test(digits) ? `A number cannot start with 0. Write 0o${digits.slice(1)} for octal, or ${decimal} for decimal.`
      : `A number cannot start with 0. Write ${decimal}.`;
  }
  return word.includes('_') ? separatorHint : '';
}

// What follows the point of a decimal number: decimal digits, grouped by _ if at all.
function pointHint(fraction) {
  if (!fraction) return 'Write digits after the point, as in 1.0.';
  if (!/^[0-9_]+$/.test(fraction)) return 'Only decimal digits follow the point, as in 10.625.';
  return separators.test(fraction) ? '' : separatorHint;
}

// A bare word that is no name (UNKNOWN_IDENT) but reads as hex: ff, DEAD, FFh.
function nameHint(word) {
  const hex = /^([0-9a-fA-F]+?)[hH]?$/.exec(word);
  return hex && word !== 'ans' ? `Hex numbers need the 0x prefix: 0x${hex[1]}.` : '';
}

export function programmerHint(input, code, { decode = false } = {}) {
  if (code === 'BAD_NUMBER' && /(?<![0-9A-Za-z_])\d+(?:\.\d+)?[eE][+-]?\d/.test(input)) {
    const write = decode ? 'Write the number in full or use Decode.' : 'Write the number in full.';
    const hex = /(?<![0-9A-Za-z_.])(\d+[eE]\d+)(?![0-9A-Za-z_.])/.exec(input);
    return `Exponents are not accepted. ${write}${hex ? ` For a hex number, write 0x${hex[1]}.` : ''}`;
  }
  if (code !== 'BAD_NUMBER' && code !== 'UNKNOWN_IDENT') return '';
  for (const match of input.matchAll(/[0-9A-Za-z_]+/g)) {
    const word = match[0];
    const after = input.slice(match.index + word.length);
    if (code === 'BAD_NUMBER' && /^\d/.test(word)) {
      // The word after a point is the fraction of the number before it, read with that number.
      if (/[0-9A-Za-z_]\.$/.test(input.slice(0, match.index))) continue;
      const hint = numberHint(word, after.startsWith('.') ? /^\.([0-9A-Za-z_]*)/.exec(after)[1] : null);
      if (hint) return hint;
    } else if (code === 'UNKNOWN_IDENT' && /^[A-Za-z_]/.test(word) && !/^\s*\(/.test(after)) {
      const hint = nameHint(word);
      if (hint) return hint;
    }
  }
  return '';
}

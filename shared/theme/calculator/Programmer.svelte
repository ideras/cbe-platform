<script>
  // The programmer panel (plan Phases 5, 6 and 6A), board B of the owner's mock: one input shared by three
  // tabs (D21). Keypad evaluates it, integers at a word size with the CPU flags of the last operator, or reals
  // (D20); IEEE 754 encodes its value or decodes its bits (D13); Radix Point converts its value exactly. Switching
  // tabs never clears the text, and a tab reads it again when it changed since that tab last read it.
  // Everything is computed in Go: CalcEvaluateInt, CalcInspect and CalcConvertFraction answer every reading as
  // strings, because a 64-bit unsigned value exceeds the JS safe integers. The signed/unsigned toggle (D15,
  // default signed) decides how /, % and >> read operands from now on, and how the IEEE 754 and Radix Point tabs
  // read an integer; it never changes the displayed result, whose bits and readings stay put.
  import { tick } from 'svelte';
  import CopyButton from './CopyButton.svelte';
  import { calcMessage, programmerHint } from './calc-messages.js';

  let { allowed = [], disabledNames = [], api, onEvaluated } = $props();
  const uid = $props.id();

  const MAX_INPUT = 200;
  const bases = [['bin', 'BIN'], ['oct', 'OCT'], ['dec', 'DEC'], ['hex', 'HEX']];
  const words = [8, 16, 32, 64];
  const ieeeFormats = ['binary16', 'bfloat16', 'binary32', 'binary64'];
  const fieldWidths = { binary16: [5, 10], bfloat16: [8, 7], binary32: [8, 23], binary64: [11, 52] };
  // The IEEE 754 tab is hidden when the exam disabled `ieee754` (D13).
  const ieeeShown = $derived(!disabledNames.includes('ieee754'));
  const tabs = $derived([
    { id: 'keypad', label: 'Keypad' },
    ...(ieeeShown ? [{ id: 'ieee754', label: 'IEEE 754' }] : []),
    { id: 'fraction', label: 'Radix Point' },
  ]);

  let base = $state('dec');
  let word = $state(32);
  let signedMode = $state(true);
  let input = $state('');
  let tab = $state('keypad');
  let result = $state(null); // the Keypad's CalcIntResult: an integer's readings, or a real's value
  let failure = $state(''); // a refused reading's message, or a Go error's text
  let failureHint = $state('');
  let warning = $state(''); // the word-size rewrap's truncation warning
  let ieeeFormat = $state('binary32');
  let direction = $state('encode');
  let ieeeResult = $state(null); // a CalcIEEEView
  let fraction = $state(null); // a CalcFractionResult
  let inputElement = $state(null);
  let tablist = $state(null);
  // What each tab last read, with the settings that decide the reading, so a tab change reads again only when
  // something changed (each reading is logged).
  const lastRead = { keypad: null, ieee754: null, fraction: null };

  const hint = $derived(tab === 'keypad' ? 'Integer operators need whole numbers.'
    : tab === 'fraction' ? 'Reads a number. Shows up to 32 fraction digits.'
    : direction === 'encode' ? 'Reads a number and shows its IEEE 754 encoding.' : 'Reads a bit pattern and shows the number it encodes.');

  // The keypad (mock board B): seven columns, digits the input base cannot read are inactive, a programmer
  // function the exam does not allow is hidden (its cell stays, so the grid keeps its shape).
  const keypad = [
    { label: 'A', digit: true }, { label: 'B', digit: true }, { label: 'C', digit: true }, { label: 'D', digit: true },
    { label: 'E', digit: true }, { label: 'F', digit: true }, { label: '⌫', action: 'backspace', aria: 'Delete the last character' },
    { label: '7', digit: true }, { label: '8', digit: true }, { label: '9', digit: true }, { label: '÷', insert: '/', aria: 'Divide' },
    { label: '%', aria: 'Remainder' }, { label: '&', aria: 'And' }, { label: '|', aria: 'Or' },
    { label: '4', digit: true }, { label: '5', digit: true }, { label: '6', digit: true }, { label: '×', insert: '*', aria: 'Multiply' },
    { label: '^', aria: 'Exclusive or' }, { label: '~', aria: 'Not' }, { label: '<<', aria: 'Shift left' },
    { label: '1', digit: true }, { label: '2', digit: true }, { label: '3', digit: true }, { label: '−', insert: '-', aria: 'Minus' },
    { label: '>>', aria: 'Shift right' }, { label: 'Ans', insert: 'ans' }, { label: '+', aria: 'Plus' },
    { label: '0', digit: true }, { label: '(' }, { label: ')' },
    { label: 'nand', name: 'nand' }, { label: 'nor', name: 'nor' }, { label: 'rol', name: 'rol' }, { label: 'ror', name: 'ror' },
    { label: '.', aria: 'Decimal point' }, { label: 'AC', action: 'clear', aria: 'Clear the input' },
  ];
  const readable = { bin: '01', oct: '01234567', dec: '0123456789', hex: '0123456789ABCDEF' };

  // Group a pure bit pattern in fours; anything else (fractions, hex readings) is shown as computed.
  const grouped = value => (value && /^[01]+$/.test(value) ? value.match(/.{1,4}/g).join(' ') : value ?? '');

  // The bit view: 32 bits a row, in nibbles, so 64 bits take two rows. A shorter word keeps bit 0 at the right
  // edge, in the same column as in a wider one. Each nibble is labeled with its lowest bit's number.
  const bitRows = $derived.by(() => {
    const binary = result?.real ? '' : result?.binary ?? '';
    const rows = [];
    for (let end = binary.length; end > 0; end -= 32) {
      const start = Math.max(0, end - 32);
      const nibbles = [];
      for (let at = start; at < end; at += 4) nibbles.push({ at, low: binary.length - at - 4, bits: binary.slice(at, at + 4).split('') });
      rows.unshift({ start: 9 - nibbles.length, nibbles });
    }
    return rows;
  });

  // The IEEE 754 view's fields as bits, from the pattern Go answered, for the labeled field strip.
  const fields = $derived.by(() => {
    if (!ieeeResult?.ok) return null;
    const [exponent, mantissa] = fieldWidths[ieeeResult.format];
    const bits = BigInt(ieeeResult.bits).toString(2).padStart(1 + exponent + mantissa, '0');
    return { sign: bits[0], exponent: grouped(bits.slice(1, 1 + exponent)), mantissa: grouped(bits.slice(1 + exponent)), widths: [exponent, mantissa] };
  });

  function readKey(which) {
    const settings = `${input}\u0000${base}\u0000${word}\u0000${signedMode}`;
    return which === 'ieee754' ? `${settings}\u0000${ieeeFormat}\u0000${direction}` : settings;
  }

  function fail(code, expression) {
    failure = calcMessage(code);
    failureHint = programmerHint(expression, code, { base, decode: ieeeShown });
  }

  function problemText(problem) { return String(problem).replace(/^Error:\s*/, ''); }

  // Enter, the keypad's =, and a tab change read the input in the active tab.
  function run() {
    if (tab === 'ieee754') return inspect();
    if (tab === 'fraction') return convertFraction();
    return evaluate();
  }

  async function evaluate() {
    const expression = input;
    if (!expression.trim()) return;
    failure = ''; failureHint = ''; warning = '';
    lastRead.keypad = readKey('keypad');
    try {
      const answer = await api.CalcEvaluateInt(expression, word, base, signedMode);
      if (answer.ok) {
        result = answer;
        onEvaluated?.({ expression, ok: true, display: answer.real ? answer.value : answer.unsigned, mode: 'programmer' });
      } else {
        result = null;
        fail(answer.code, expression);
        onEvaluated?.({ expression, ok: false, code: answer.code, message: failure, mode: 'programmer' });
      }
    } catch (problem) {
      result = null;
      failure = problemText(problem);
    }
  }

  async function inspect() {
    const expression = input;
    if (!expression.trim()) return;
    failure = ''; failureHint = '';
    lastRead.ieee754 = readKey('ieee754');
    try {
      const answer = await api.CalcInspect(expression, ieeeFormat, direction, word, base, signedMode);
      if (answer.ok) {
        ieeeResult = answer;
        onEvaluated?.({ expression, ok: true, display: answer.bits, mode: 'programmer', view: 'ieee' });
      } else {
        ieeeResult = null;
        fail(answer.code, expression);
        onEvaluated?.({ expression, ok: false, code: answer.code, message: failure, mode: 'programmer', view: 'ieee' });
      }
    } catch (problem) {
      ieeeResult = null;
      failure = problemText(problem);
    }
  }

  async function convertFraction() {
    const expression = input;
    if (!expression.trim()) return;
    failure = ''; failureHint = '';
    lastRead.fraction = readKey('fraction');
    try {
      const answer = await api.CalcConvertFraction(expression, word, base, signedMode);
      if (answer.ok) {
        fraction = answer;
        onEvaluated?.({ expression, ok: true, display: answer.binary, mode: 'programmer' });
      } else {
        fraction = null;
        fail(answer.code, expression);
        onEvaluated?.({ expression, ok: false, code: answer.code, message: failure, mode: 'programmer' });
      }
    } catch (problem) {
      fraction = null;
      failure = problemText(problem);
    }
  }

  // A tab change is recorded (D22) and reads the input in the new tab when it changed since that tab's last
  // reading, so a value carries from one tab to the next.
  function switchTab(next) {
    if (next === tab) return;
    api.CalcTabChanged?.(tab, next)?.catch?.(() => {});
    tab = next;
    failure = ''; failureHint = '';
    if (input.trim() && lastRead[next] !== readKey(next)) run();
  }

  // Left and right move between the tabs; Enter and Space select one (the button's own click).
  function tabKeys(event) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    const buttons = [...tablist.querySelectorAll('[role="tab"]')];
    const index = buttons.indexOf(event.currentTarget);
    buttons[(index + (event.key === 'ArrowRight' ? 1 : buttons.length - 1)) % buttons.length].focus();
  }

  function setFormat(next) {
    if (next === ieeeFormat) return;
    ieeeFormat = next;
    if (input.trim()) inspect();
  }

  function setDirection(next) {
    if (next === direction) return;
    direction = next;
    if (input.trim()) inspect();
  }

  // Changing the word size re-wraps the last integer result in the frontend, from the exact bits Go sent (a
  // decimal string); the flags were those of the last operator and stay as they were. A shrunk word that loses
  // bits is warned about. A real has no word size.
  function setWord(next) {
    warning = '';
    if (result && !result.real) {
      const value = BigInt(result.bits);
      if (value >> BigInt(next) !== 0n) warning = `The value was truncated to ${next} bits.`;
      const bits = value & ((1n << BigInt(next)) - 1n);
      result = { ...result, bits: bits.toString(10),
        unsigned: bits.toString(10),
        signed: (bits >= (1n << BigInt(next - 1)) ? bits - (1n << BigInt(next)) : bits).toString(10),
        hex: '0x' + bits.toString(16).toUpperCase().padStart(next / 4, '0'),
        octal: bits.toString(8).padStart(Math.ceil(next / 3), '0'),
        binary: bits.toString(2).padStart(next, '0') };
    }
    word = next;
  }

  // Clicking a bit flips it, writes the new value into the input as a literal in the current input base, and
  // evaluates it — logged like any other evaluation (plan Phase 5); another active tab reads it too.
  async function flip(index) {
    const bits = (result?.binary ?? '').split('');
    if (index >= bits.length) return;
    bits[index] = bits[index] === '1' ? '0' : '1';
    const value = BigInt('0b' + bits.join(''));
    input = { dec: value.toString(10), hex: '0x' + value.toString(16).toUpperCase(), oct: '0o' + value.toString(8), bin: '0b' + bits.join('') }[base];
    await evaluate();
    if (tab !== 'keypad') run();
  }

  function focusInput() { tick().then(() => inputElement?.focus()); }

  function insert(text) {
    if (input.length + text.length > MAX_INPUT) {
      failure = calcMessage('TOO_LONG'); failureHint = '';
      return;
    }
    input += text;
    focusInput();
  }

  function press(key) {
    if (key.digit && !readable[base].includes(key.label)) return; // inactive in this base
    if (key.action === 'backspace') { input = input.slice(0, -1); focusInput(); return; }
    if (key.action === 'clear') { input = ''; failure = ''; failureHint = ''; focusInput(); return; }
    if (key.name) { insert(`${key.name}(`); return; }
    insert(key.insert ?? key.label);
  }

  // In every tab, typing goes to the shared input: a printable key pressed on a button of the panel appends to
  // it, and Backspace deletes from it. Space and Enter keep selecting the focused button.
  function typeIntoInput(event) {
    if (event.target === inputElement || event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.key === 'Backspace') {
      event.preventDefault();
      input = input.slice(0, -1);
      focusInput();
    } else if (event.key.length === 1 && event.key !== ' ') {
      event.preventDefault();
      insert(event.key);
    }
  }
</script>

<!-- The keydown handler only routes typing to the shared input; the controls keep their own keys. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="prog" onkeydown={typeIntoInput}>
  <div class="rows">
    <div class="row">
      <span class="seg" role="group" aria-label="Input base">{#each bases as [value, label] (value)}
        <button type="button" aria-pressed={base === value} onclick={() => base = value}>{label}</button>
      {/each}</span>
      <span class="seg" role="group" aria-label="Signed arithmetic" title="Decides how ÷, % and » read operands from now on, and how the IEEE 754 and Radix Point tabs read an integer.">
        <button type="button" aria-pressed={signedMode} onclick={() => signedMode = true}>Signed</button>
        <button type="button" aria-pressed={!signedMode} onclick={() => signedMode = false}>Unsigned</button>
      </span>
    </div>
    <div class="row">
      <span class="seg" role="group" aria-label="Word size">{#each words as value (value)}
        <button type="button" aria-pressed={word === value} onclick={() => setWord(value)}>{value}</button>
      {/each}</span>
      <span class="flags" aria-label="Flags">
        <span class="chip" class:on={result?.carry}>CF {result?.carry ? '1' : '0'}</span>
        <span class="chip" class:on={result?.overflow}>OF {result?.overflow ? '1' : '0'}</span>
      </span>
    </div>
  </div>

  <div class="display">
    <input class="calc-input" bind:this={inputElement} bind:value={input} maxlength={MAX_INPUT} spellcheck="false" autocomplete="off"
      aria-label="Programmer expression" aria-describedby="{uid}-hint" placeholder="e.g. 0xFF &amp; 0x0F, 1 &lt;&lt; 4, 10.625"
      onkeydown={event => { if (event.key === 'Enter') { event.preventDefault(); run(); } }} />
    <p class="hint" id="{uid}-hint">{hint}</p>
    <p class="calc-result" aria-live="polite">
      {#if failure}<span class="error">{failure}</span>{#if failureHint}<span class="hint-line">{failureHint}</span>{/if}{/if}
    </p>
    <p class="readings">
      {#if result?.real}
        <span><strong>Value</strong> {result.value}<CopyButton value={result.value} label="Copy answer" /></span>
        <span><strong>{signedMode ? 'DEC signed' : 'DEC'}</strong> <span class="dash">—</span></span>
        <span><strong>DEC unsigned</strong> <span class="dash">—</span></span>
        <span><strong>HEX</strong> <span class="dash">—</span></span>
        <span><strong>OCT</strong> <span class="dash">—</span></span>
        <span><strong>BIN</strong> <span class="dash">—</span></span>
      {:else if result}
        <span><strong>{signedMode ? 'DEC signed' : 'DEC'}</strong> {result.signed}<CopyButton value={result.signed} label="Copy signed decimal" /></span>
        <span><strong>DEC unsigned</strong> {result.unsigned}<CopyButton value={result.unsigned} label="Copy unsigned decimal" /></span>
        <span><strong>HEX</strong> {result.hex}<CopyButton value={result.hex} label="Copy hexadecimal" /></span>
        <span><strong>OCT</strong> {result.octal}<CopyButton value={result.octal} label="Copy octal" /></span>
        <span><strong>BIN</strong> {grouped(result.binary)}<CopyButton value={result.binary} label="Copy binary" /></span>
      {/if}
    </p>
    {#if result?.real}<p class="note">Whole numbers only. The Radix Point and IEEE 754 tabs show this value.</p>{/if}
  </div>
  {#if warning}<p class="error" role="alert">{warning}</p>{/if}
  {#if result?.real}
    <p class="bitsoff">Bit view needs a whole number.</p>
  {:else if result}
    <div class="bitgrid" role="group" aria-label="Bits of the last result, most significant first">
      {#each bitRows as row (row.nibbles[0].at)}
        <div class="bitrow">
          {#each row.nibbles as nibble, n (nibble.at)}
            <div class="nibble" style:grid-column={row.start + n}>
              {#each nibble.bits as bit, i (i)}
                <button type="button" class:one={bit === '1'} aria-label="Bit {nibble.low + 3 - i}, {bit === '1' ? 'clear it' : 'set it'}"
                  onclick={() => flip(nibble.at + i)}>{bit}</button>
              {/each}
              <span class="pos" aria-hidden="true">{nibble.low}</span>
            </div>
          {/each}
        </div>
      {/each}
    </div>
  {/if}

  <div class="seg tabs" role="tablist" aria-label="Programmer tools" bind:this={tablist}>
    {#each tabs as item (item.id)}
      <button type="button" role="tab" id="{uid}-tab-{item.id}" aria-controls="{uid}-panel-{item.id}" aria-selected={tab === item.id}
        onclick={() => switchTab(item.id)} onkeydown={tabKeys}>{item.label}</button>
    {/each}
  </div>

  {#if tab === 'keypad'}
    <div class="panel" role="tabpanel" id="{uid}-panel-keypad" aria-labelledby="{uid}-tab-keypad">
      <div class="keys" role="group" aria-label="Programmer keys">
        {#each keypad as key (key.label)}
          {#if key.name && !allowed.includes(key.name)}
            <span class="key-gap" aria-hidden="true"></span>
          {:else}
            <button type="button" class="key" class:fn={key.name} class:sym={!key.digit && !key.name && !key.action} class:clear={key.action === 'clear'}
              aria-label={key.aria} aria-disabled={key.digit && !readable[base].includes(key.label) ? 'true' : undefined}
              onclick={() => press(key)}>{key.label}</button>
          {/if}
        {/each}
        <button type="button" class="key equals" aria-label="Evaluate" onclick={evaluate}>=</button>
      </div>
    </div>
  {:else if tab === 'ieee754'}
    <div class="panel ieee" role="tabpanel" id="{uid}-panel-ieee754" aria-labelledby="{uid}-tab-ieee754">
      <div class="row">
        <span class="seg" role="group" aria-label="Format">{#each ieeeFormats as format (format)}
          <button type="button" aria-pressed={ieeeFormat === format} onclick={() => setFormat(format)}>{format}</button>
        {/each}</span>
        <span class="seg" role="group" aria-label="Direction">
          <button type="button" aria-pressed={direction === 'encode'} onclick={() => setDirection('encode')}>Encode</button>
          <button type="button" aria-pressed={direction === 'decode'} onclick={() => setDirection('decode')}>Decode</button>
        </span>
        <button type="button" class="run" onclick={inspect}>Inspect</button>
      </div>
      {#if ieeeResult?.ok && fields}
        <div class="fields">
          <div class="field sign"><span class="label">Sign</span><code>{fields.sign}</code></div>
          <div class="field exp"><span class="label">Exponent, {fields.widths[0]} bits</span><code>{fields.exponent}</code></div>
          <div class="field man"><span class="label">Mantissa, {fields.widths[1]} bits</span><code>{fields.mantissa}</code></div>
        </div>
        <p class="readings">
          <span><strong>Bits</strong> {ieeeResult.bits}<CopyButton value={ieeeResult.bits} label="Copy IEEE bits" /></span>
          <span><strong>Sign</strong> {ieeeResult.sign}</span>
          <span><strong>Exponent field</strong> {ieeeResult.exponent_field}</span>
          <span><strong>Unbiased exponent</strong> {ieeeResult.unbiased_exponent}</span>
          <span><strong>Mantissa field</strong> {ieeeResult.mantissa_field}</span>
          <span><strong>Class</strong> {ieeeResult.class}</span>
          <span><strong>Value</strong> {ieeeResult.value}<CopyButton value={ieeeResult.value} label="Copy IEEE value" /></span>
        </p>
        {#if ieeeResult.rounded}<p class="note">The input had more precision than {ieeeResult.format}; this is the rounded value.</p>{/if}
      {/if}
    </div>
  {:else}
    <div class="panel fraction" role="tabpanel" id="{uid}-panel-fraction" aria-labelledby="{uid}-tab-fraction">
      <div class="row"><span></span><button type="button" class="run" onclick={convertFraction}>Convert</button></div>
      {#if fraction}
        <p class="readings">
          <span><strong>BIN</strong> {grouped(fraction.binary)}<CopyButton value={fraction.binary} label="Copy binary fraction" /></span>
          <span><strong>OCT</strong> {fraction.octal}<CopyButton value={fraction.octal} label="Copy octal fraction" /></span>
          <span><strong>HEX</strong> {fraction.hex}<CopyButton value={fraction.hex} label="Copy hexadecimal fraction" /></span>
        </p>
        <p class="note">{fraction.binary.endsWith('...') || fraction.octal.endsWith('...') || fraction.hex.endsWith('...')
          ? 'The expansion goes on: it stops after 32 digits, followed by ...' : 'Exact in all three bases.'}</p>
      {/if}
    </div>
  {/if}
</div>

<style>
  .prog { display: grid; gap: var(--space-2); }
  .rows { display: grid; gap: var(--space-2); }
  .row { display: flex; flex-wrap: wrap; gap: var(--space-2); align-items: center; justify-content: space-between; }
  .seg { display: inline-flex; gap: 2px; padding: 2px; border: 1px solid var(--ink-2); border-radius: var(--radius-sm); background: var(--color-surface-sunken); }
  .seg button { margin: 0; padding: .15rem .55rem; border: 0; font-size: var(--text-sm); background: var(--transparent); }
  .seg button[aria-pressed='true'], .seg button[aria-selected='true'] { background: var(--ink); color: var(--surface); }
  .tabs { justify-self: start; }
  .flags { display: inline-flex; gap: var(--space-1); }
  .chip { font-family: var(--font-mono, monospace); font-size: var(--text-xs); padding: .1rem .55rem; border-radius: 999px; background: var(--color-surface-sunken); color: var(--color-text-muted); }
  .chip.on { background: var(--coral-tint); color: var(--coral); font-weight: 600; }
  .display { display: grid; gap: var(--space-1); padding: var(--space-2) var(--space-3); border-radius: var(--radius-md); background: var(--color-surface-sunken); }
  .display .calc-input { width: 100%; text-align: right; font-family: var(--font-mono, monospace); }
  .hint { margin: 0; font-size: var(--text-xs); color: var(--color-text-muted); text-align: right; }
  .calc-result { margin: 0; min-height: 1.2em; display: grid; gap: 2px; }
  .hint-line { font-size: var(--text-sm); color: var(--color-text-muted); }
  .readings { display: grid; gap: var(--space-1); margin: 0; font-family: var(--font-mono, monospace); overflow-wrap: anywhere; }
  .readings strong { display: inline-block; min-width: 7rem; color: var(--color-text-muted); font-weight: 600; font-family: var(--font-ui, sans-serif); font-size: var(--text-sm); }
  .dash { color: var(--color-text-muted); }
  .note { margin: 0; font-size: var(--text-sm); color: var(--color-text-muted); }
  .bitsoff { margin: 0; padding: var(--space-2) var(--space-3); border: 1px dashed var(--color-border); border-radius: var(--radius-sm); color: var(--color-text-muted); font-size: var(--text-sm); }
  .bitgrid { display: grid; gap: var(--space-1); }
  .bitrow { display: grid; grid-template-columns: repeat(8, minmax(0, 1fr)); gap: var(--space-2); }
  .nibble { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 1px; }
  .nibble button { min-width: 0; min-height: 0; height: 1.55rem; margin: 0; padding: 0; border-color: var(--ink-2); background: var(--page);
    font-family: var(--font-mono, monospace); font-size: var(--text-sm); font-weight: 600; }
  .nibble button:hover:not(:disabled) { border-color: var(--ink-2); }
  .nibble button { border-radius: 3px; }
  .nibble .one { background: var(--ink); border-color: var(--ink); color: var(--surface); }
  .nibble .one:hover:not(:disabled) { background: var(--ink); }
  .pos { grid-column: 4; justify-self: center; font-size: var(--text-xs); line-height: 1; color: var(--color-text-muted); }
  .error { color: var(--coral); margin: 0; }
  .panel { display: grid; gap: var(--space-2); align-content: start; min-height: 236px; }
  .keys { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: var(--space-1); }
  .key { margin: 0; padding: .35rem 0; font-family: var(--font-mono, monospace); font-size: var(--text-sm); }
  .key.sym { background: var(--transparent); }
  .key.fn { background: var(--sea); color: var(--sea-ink); }
  .key.clear { color: var(--coral); }
  .key.clear:hover { background: var(--coral-tint); }
  .key[aria-disabled='true'] { opacity: .4; cursor: not-allowed; }
  .key.equals { grid-column: span 5; background: var(--ocean); border-color: var(--ocean); color: var(--surface); }
  .key.equals:hover { background: var(--ocean-strong); border-color: var(--ocean-strong); }
  .run { margin: 0; }
  .fields { display: grid; grid-template-columns: auto auto minmax(0, 1fr); gap: var(--space-1); }
  .field { display: grid; gap: 2px; padding: var(--space-1) var(--space-2); border: 1px solid var(--color-border); border-radius: var(--radius-sm); overflow-wrap: anywhere; }
  .field .label { font-size: var(--text-xs); font-weight: 600; color: var(--color-text-muted); }
  .field code { font-family: var(--font-mono, monospace); font-size: var(--text-sm); }
  .field.sign { background: var(--color-warning-bg); }
  .field.exp { background: var(--sea); }
  .field.man { background: var(--surface); }
</style>

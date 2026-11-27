<script>
  import Icon from '../../../../shared/theme/Icon.svelte';

  let { text, label, json, stderr, truncated } = $props();

  // Change only whitespace outside JSON strings. Grade output can contain numeric
  // literals which would lose precision if parsed and serialized by JavaScript.
  function formatJSON(source) {
    let depth = 0;
    const newline = () => '\n' + '  '.repeat(Math.min(depth, 16));
    return source.replace(/"(?:\\.|[^"\\])*"|\{\s*\}|\[\s*\]|[{}[\],:]|\s+/g, token => {
      switch (token) {
        case '{': case '[': depth++; return token + newline();
        case '}': case ']': depth = Math.max(0, depth - 1); return newline() + token;
        case ',': return ',' + newline();
        case ':': return ': ';
        default: return token.startsWith('"') ? token : token.trim();
      }
    });
  }

  function jsonTokens(line) {
    const tokens = [];
    let last = 0;
    for (const match of line.matchAll(/"(?:\\.|[^"\\])*"(?:\s*:)?|\b(?:true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g)) {
      const value = match[0];
      if (match.index > last) tokens.push({ text: line.slice(last, match.index) });
      tokens.push({ text: value, tone: value.endsWith(':') ? 'key' : value[0] === '"' ? 'string' : 'value' });
      last = match.index + value.length;
    }
    if (last < line.length) tokens.push({ text: line.slice(last) });
    return tokens;
  }

  function diagnostic(line) {
    if (/\bwarning:/i.test(line)) return 'warning';
    if (/\berror\b|\w+Error:|Traceback \(|timed out|\braised\b/i.test(line)) return 'error';
    if (/^\[grade\] (build ok|done)\b/.test(line)) return 'ok';
    if (/^\[grade\]/.test(line)) return 'dim';
    return '';
  }

  // Keep large captures selectable without creating a DOM node for every line or token.
  const lines = $derived.by(() => {
    if (!text || text.length > 65536) return null;
    const source = json ? formatJSON(text) : text;
    const rows = source.split(/\r?\n/);
    if (rows.length > 1500) return null;
    return rows.map(line => ({ line, tone: stderr ? diagnostic(line) : '', tokens: json ? jsonTokens(line) : null }));
  });
  const warnings = $derived(lines?.filter(row => row.tone === 'warning').length ?? 0);
  const errors = $derived(lines?.filter(row => row.tone === 'error').length ?? 0);
</script>

{#if truncated}<p class="notice">This stream was cut at the output limit.</p>{/if}
{#if stderr && (warnings || errors)}
  <p class="notice"><Icon name="warning" size={16}/><span>{warnings} warning {warnings === 1 ? 'line' : 'lines'} · {errors} error or timeout {errors === 1 ? 'line' : 'lines'}</span></p>
{/if}
<!-- svelte-ignore a11y_no_noninteractive_tabindex (The captured stream is keyboard scrollable.) -->
<div class="terminal" role="region" tabindex="0" aria-label="{label} of the run">
  {#if lines}
    {#each lines as row, index}
      <div class="line"><span class="no" aria-hidden="true">{index + 1}</span><span class="text {row.tone}">{#if row.tokens}{#each row.tokens as token}<span class={token.tone}>{token.text}</span>{/each}{:else}{row.line}{/if}</span></div>
    {/each}
  {:else}<pre>{text || `(${label.toLowerCase()} was empty)`}</pre>{/if}
</div>

<style>
  .notice { display: flex; align-items: center; gap: .5rem; margin: 0 0 .75rem; padding: .5rem .75rem; border: 1px solid var(--warn-border); border-left: 4px solid var(--warn); border-radius: 8px; background: var(--warn-fill); color: var(--warn-text); font-size: .83rem; }
  .terminal { padding: .7rem .75rem .8rem .25rem; min-height: 9rem; border-radius: 10px; background: var(--term-ground); color: var(--term-text); font: .82rem/1.55 var(--mono); }
  .line { display: grid; grid-template-columns: 3.2em minmax(0, 1fr); }
  .no { user-select: none; text-align: right; padding-right: .8em; color: var(--term-dim); }
  .text, pre { white-space: pre-wrap; overflow-wrap: anywhere; }
  pre { margin: 0; padding: 0 .6rem; font: inherit; border: 0; color: inherit; }
  .key { color: var(--term-cyan); } .string, .ok { color: var(--term-green); } .value, .warning { color: var(--term-yellow); }
  .error { color: var(--term-red); } .dim { color: var(--term-dim); }
</style>

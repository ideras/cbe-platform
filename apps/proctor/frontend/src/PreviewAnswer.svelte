<script>
  let { slot, number, fieldNames, answers, onChange } = $props();
  const value = $derived(answers.get(slot.id) ?? slot.starter ?? '');
  const tried = $derived(answers.has(slot.id) && value.trim() !== '');
  const blank = $derived(slot.kind === 'blank');
  const code = $derived(slot.kind === 'code');
  const rows = $derived(slot.lines || (code ? 12 : 6));
  const kindLabel = $derived(code ? `code${slot.language ? ` (${slot.language})` : ''}` : `text, ${rows} lines`);
  const update = event => onChange(slot, event.currentTarget.value);
</script>

{#if fieldNames}
  <code class="field-tag" title="Answer field name">{slot.id}{blank ? '' : ` · ${kindLabel}`}</code>
  {#if tried}<span class="tried">Your try: <code>{value}</code></span>{/if}
{:else if blank}
  <input class="blank" style:width="{Math.min(34, Math.max(17, value.length + 2))}ch"
    aria-label="Answer {number}" placeholder="Your answer" autocomplete="off" spellcheck="false" {value} oninput={update} />
  {#if tried}<span class="tried">Filled in (not saved)</span>{/if}
{:else}
  {#if slot.language}<span class="language">{slot.language}</span>{/if}
  <textarea class:code aria-label="Answer {number}" placeholder="Your answer" autocomplete="off" spellcheck="false" {rows} {value} oninput={update}></textarea>
  {#if tried}<span class="tried">Filled in (not saved)</span>{/if}
{/if}

<style>
  .blank, textarea { max-width: 100%; background: var(--bg-primary); font-family: var(--font-mono); font-size: var(--text-sm); }
  .blank { margin: .15rem; padding: .3rem .55rem; vertical-align: baseline; }
  textarea { display: block; width: 100%; margin: .25rem 0; resize: vertical; }
  textarea.code { tab-size: 4; }
  .language { display: block; color: var(--text-muted); font-size: .73rem; font-weight: 700; text-transform: uppercase; }
  .field-tag { display: inline-block; padding: .12rem .5rem; border: 1px dashed var(--warn); border-radius: 7px; background: var(--warn-fill); color: var(--warn-text); font: 700 .82rem var(--font-mono); }
  .tried { margin-inline-start: .35rem; color: var(--ok-text); font-size: .76rem; overflow-wrap: anywhere; }
  .tried code { white-space: pre-wrap; }
</style>

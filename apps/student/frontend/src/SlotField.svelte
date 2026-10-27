<script>
  // One answer field, placed by RichText where the exam text marks it: a blank inside the text, a text box or a code editor.
  import CodeEditor from './CodeEditor.svelte';

  let { slot, value, locked, onChange } = $props();
  const width = $derived(Math.min(Math.max(value.length + 2, 8), 40));
</script>

{#if slot.kind === 'blank'}
  <input class="blank" style:width="{width}ch" aria-label="Answer for {slot.id}" {value} readonly={locked} autocomplete="off" spellcheck="false"
    oninput={event => onChange(slot.id, event.currentTarget.value)} />
{:else if slot.kind === 'text'}
  <textarea class="text-answer" aria-label="Answer for {slot.id}" rows={slot.lines || 6} {value} readonly={locked} spellcheck="false"
    oninput={event => onChange(slot.id, event.currentTarget.value)}></textarea>
{:else}
  <CodeEditor slotId={slot.id} language={slot.language} rows={slot.lines} {value} {locked} {onChange} />
{/if}

<style>
  .blank {
    display: inline-block; max-width: 100%; min-height: 0; margin: .1rem .15rem; padding: .1rem .45rem;
    font-family: var(--font-mono); font-size: var(--text-sm); vertical-align: baseline;
  }
  .text-answer { display: block; width: 100%; min-width: 0; margin: var(--space-2) 0; font-family: var(--font-mono); resize: vertical; }
  .blank[readonly], .text-answer[readonly] { background: var(--color-disabled-bg); color: var(--color-disabled-text); }
</style>

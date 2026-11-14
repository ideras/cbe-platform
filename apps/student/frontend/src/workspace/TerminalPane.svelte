<script>
  import Icon from '../../../../../shared/theme/Icon.svelte';
  import TerminalView from './TerminalView.svelte';
  import { MAX_TERMINALS } from './model.js';

  // The terminals: a tab for each (up to four), and for each the program's output. Run and Check open a tab of their own, which ends
  // with the program; an exam without a terminal has only those, with no keyboard.
  let { model, terminals, activeTerm, terminalAllowed, readOnly } = $props();
  let refs = $state({});
  const active = $derived(terminals.find(item => item.term === activeTerm) ?? null);
  const running = $derived(terminals.filter(item => !item.exited).length);

  const ending = item => item.oom ? 'ran out of memory and was stopped' : item.signal ? `was stopped by ${item.signal}` : `ended with code ${item.exitCode}`;
  const mark = item => !item.exited ? '' : item.exitCode === 0 ? '✓' : '✗';
  function select(term) { model.selectTerminal(term); queueMicrotask(() => refs[term]?.focus()); }
  function tabKeys(event, index) {
    const go = delta => { event.preventDefault(); const next = terminals[(index + delta + terminals.length) % terminals.length]; if (next) { model.selectTerminal(next.term); queueMicrotask(() => document.getElementById(`term-tab-${next.term}`)?.focus()); } };
    if (event.key === 'ArrowRight') go(1);
    else if (event.key === 'ArrowLeft') go(-1);
  }
</script>
<section class="terminals" aria-label="Terminals">
  <div class="tabbar">
    <div class="tabs" role="tablist" aria-label="Terminal tabs">
      {#each terminals as item, index (item.term)}
        <button type="button" role="tab" class="tab" class:front={item.term === activeTerm} id="term-tab-{item.term}" aria-selected={item.term === activeTerm}
          tabindex={item.term === activeTerm ? 0 : -1} onclick={() => select(item.term)} onkeydown={event => tabKeys(event, index)}>
          {item.title}{#if item.exited}<span class="mark" class:bad={item.exitCode !== 0} role="img" aria-label={ending(item)}>{mark(item)}</span>{/if}
          <span class="close" aria-hidden="true" onclick={event => { event.stopPropagation(); model.closeTerminal(item.term); }}><Icon name="close" size={14} /></span>
        </button>
      {/each}
      {#if terminals.length === 0}<span class="none">{terminalAllowed ? 'No terminal open. Use + to open one, or Run and Check above.' : 'Run and Check show their output here.'}</span>{/if}
    </div>
    {#if terminalAllowed}
      <button type="button" class="tool" disabled={readOnly || running >= MAX_TERMINALS} onclick={() => model.openTerminal()} aria-label="New terminal" title={running >= MAX_TERMINALS ? `${MAX_TERMINALS} terminals are open` : 'New terminal'}><Icon name="plus" size={16} /></button>
    {/if}
    {#if active}<button type="button" class="tool" aria-label="Close {active.title}" title="Close this terminal" onclick={() => model.closeTerminal(active.term)}><Icon name="close" size={16} /></button>{/if}
  </div>
  <div class="views">
    {#each terminals as item (item.term)}
      <div class="slot" hidden={item.term !== activeTerm}>
        <TerminalView {model} info={item} active={item.term === activeTerm} {readOnly} keyboard={terminalAllowed && !item.exited} bind:this={refs[item.term]} />
      </div>
    {/each}
  </div>
  {#if active?.exited}<p class="ended" role="status">{active.title} {ending(active)}.</p>{/if}
</section>
<style>
  .terminals { display: grid; grid-template-rows: auto minmax(0, 1fr) auto; min-height: 0; min-width: 0; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); overflow: hidden; }
  .tabbar { display: flex; align-items: stretch; background: var(--bg-secondary); border-bottom: 1px solid var(--border-subtle); min-height: 2.1rem; }
  .tabs { display: flex; flex: 1 1 auto; align-items: stretch; gap: 1px; min-width: 0; overflow-x: auto; }
  .tab { display: inline-flex; align-items: center; flex: none; gap: .25rem; margin: 0; padding: .35rem .5rem .35rem .7rem; border: 0; border-bottom: 3px solid var(--transparent); border-radius: 0; background: var(--bg-secondary); box-shadow: none; color: var(--text-main); font-size: var(--text-sm); }
  .tab.front { background: var(--bg-raised); border-bottom-color: var(--action-primary); font-weight: 700; }
  .tab:focus-visible, .tool:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .close { display: inline-flex; padding: .1rem; border-radius: 4px; color: var(--text-muted); }
  .close:hover { color: var(--alert-text); background: var(--hover-fill); }
  .tool { flex: none; margin: 0; padding: .25rem .6rem; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); }
  .tool:disabled { opacity: .45; }
  .mark { margin-left: .35rem; color: var(--ok-text); }
  .mark.bad { color: var(--alert-text); }
  .none { padding: .5rem .75rem; color: var(--text-muted); font-size: var(--text-sm); }
  .views, .slot { min-height: 0; height: 100%; }
  .ended { margin: 0; padding: .3rem .75rem; border-top: 1px solid var(--border-subtle); color: var(--text-muted); font-size: var(--text-sm); }
</style>

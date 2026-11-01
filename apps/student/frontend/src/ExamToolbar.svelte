<script>
  import { onMount, untrack } from 'svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { effectiveRemaining, timeChip, TimeWarnings, parseTime } from '../../../../shared/exam/timing.js';
  let { title, position = '', status, statusObservation, ageMillis = 0, stale = false, locked = false, complete = false,
    saveText = '', scaleIndex = 1, onSmaller, onLarger, calculator = false, onCalculator, onWarning = () => {}, calcButton = $bindable(null) } = $props();
  let toolbar;
  let remaining = $state(null);
  let reported = null, arrival = 0, reportAge = 0;
  const warnings = new TimeWarnings();
  const key = $derived(statusObservation || status?.server_time);
  $effect(() => {
    key;
    untrack(() => {
      reported = status; arrival = performance.now(); reportAge = ageMillis;
      if (!stale) remaining = effectiveRemaining(reported, reportAge, 0);
    });
  });
  const chip = $derived(complete ? { kind: 'submitted', text: 'Submitted' } : timeChip(status, remaining, stale));
  $effect(() => {
    const notices = warnings.update(status, remaining, stale);
    for (const notice of notices) untrack(() => onWarning(notice));
  });
  onMount(() => {
    const timer = setInterval(() => {
      if (!stale && !complete) remaining = effectiveRemaining(reported, reportAge, performance.now() - arrival);
    }, 1000);
    const resize = new ResizeObserver(() => document.documentElement.style.setProperty('--toolbar-h', `${toolbar.getBoundingClientRect().height}px`));
    resize.observe(toolbar);
    return () => { clearInterval(timer); resize.disconnect(); document.documentElement.style.removeProperty('--toolbar-h'); };
  });
</script>
<header class="toolbar" aria-label="Exam toolbar" bind:this={toolbar}>
  <h2 title={title}>{title}</h2>
  {#if position && !complete}<span class="position">{position}</span>{/if}
  <span class="time-chip {chip.kind}" aria-label="Remaining time">{chip.text}</span>
  {#if locked && !complete}<span>Locked</span>{/if}
  {#if status?.ends_at}<span class="ends">Ends {new Date(parseTime(status.ends_at)).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}</span>{/if}
  {#if !complete}<span class="save" title={saveText}>{saveText}</span>
    <div class="size"><button aria-label="Smaller text" disabled={scaleIndex === 0} onclick={onSmaller}>A−</button><button aria-label="Larger text" disabled={scaleIndex === 4} onclick={onLarger}>A+</button></div>
  {/if}
  {#if calculator}<button class="calc-open" bind:this={calcButton} onclick={onCalculator}><Icon name="calculator" size={18} />Calculator</button>{/if}
</header>
<style>
  .toolbar { position: sticky; top: 0; z-index: 40; display: flex; align-items: center; flex-wrap: wrap; gap: .45rem .7rem; padding: .65rem .8rem; background: var(--header-strong); color: var(--on-dark); border-radius: var(--radius-ctl); box-shadow: var(--shadow-raised); }
  h2 { flex: 1 1 10rem; min-width: 0; margin: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--accent-cyan-soft); font-size: 1.1rem; }
  .position, .save, .ends { font-size: var(--text-sm); }
  .time-chip { padding: .2rem .5rem; border-radius: 6px; font-family: var(--font-mono); background: var(--chip-on-dark); color: var(--on-dark); white-space: nowrap; }
  .warning, .stale { background: var(--warn-fill); color: var(--warn-text); }
  .grace { background: var(--alert-fill); color: var(--alert-text); }
  button { margin: 0; padding-inline: .6rem; background: var(--chip-on-dark); border-color: var(--cyan-chip-border); color: var(--on-dark); }
  button:hover:not(:disabled) { background: var(--side-active); color: var(--on-dark); }
  button:focus-visible { outline-color: var(--accent-cyan-soft); }
  .size { display: flex; gap: .25rem; }
  .calc-open { display: inline-flex; gap: .4rem; align-items: center; border-radius: 999px; }
  @media (max-width: 520px) { h2 { flex-basis: 100%; } .save { margin-left: auto; } .toolbar { gap: .35rem .5rem; } }
</style>

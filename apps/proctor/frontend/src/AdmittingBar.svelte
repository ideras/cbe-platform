<script>
  import { localTime } from './dates.js';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { monitorExam } from './monitor.js';
  import { message } from './errors.js';
  // What students are being admitted to right now, at the top of every page: the published exam, its class and how many students
  // are working in it. Only one exam is published at a time, whichever class it belongs to.
  let { view, onSelected, onOpenClass } = $props();
  let working = $state(false);
  let problem = $state('');
  const catalog = $derived(view.catalog);
  const exam = $derived(catalog?.exams?.find(entry => entry.published));
  const locked = $derived(working || view.busy);
  // How many students of the published exam are locked right now; the exam list counts them for every exam.
  const lockedCount = $derived(exam?.locked_sessions ?? 0);
  async function monitor() {
    working = true; problem = '';
    try { await monitorExam(view, exam.exam_id, onSelected); } catch (err) { problem = message(err); } finally { working = false; }
  }
</script>
<section class="admitting" class:closed={!exam} aria-label="Now admitting">
  {#if exam}
    <span class="dot" aria-hidden="true"></span>
    <div class="line"><span class="label">Admitting now</span>{' '}<span class="what">{exam.title}<span>{` · ${catalog.current_class_name}`}</span></span></div>{' '}
    <span class="count" class:live={exam.active_sessions > 0}>{exam.active_sessions} working</span>
    {#if lockedCount}<span class="count">{lockedCount} locked</span>{/if}
    {#if exam.ends_at}<span class="count">Ends {localTime(exam.ends_at)}</span>{/if}
    <div class="buttons">
      <button type="button" disabled={locked} title="Open the monitoring window for {exam.title}" onclick={monitor}><Icon name="monitor" size={15} />Monitor</button>
      <button type="button" disabled={locked} title="Open {catalog.current_class_name}, on its exams" onclick={() => onOpenClass(catalog.current_class_id)}>Open class<Icon name="chevron" size={15} /></button>
    </div>
    {#if problem}<p class="problem" role="alert">{problem}</p>{/if}
  {:else}
    <span class="dot" aria-hidden="true"></span>
    <p class="line">Admission closed</p>
  {/if}
</section>
<style>
  /* Open admission is the mockups' success state; closed is a neutral raised strip with a grey dot. */
  .admitting { display: flex; flex-wrap: wrap; align-items: center; gap: .85rem; padding: .65rem .75rem .65rem 1rem; background: var(--ok-fill); border: 1px solid var(--ok-border); border-left: 5px solid var(--ok); border-radius: 12px; color: var(--ok-text); box-shadow: var(--shadow-card); }
  .admitting.closed { background: var(--bg-raised); border-color: var(--border-subtle); color: var(--text-muted); box-shadow: var(--shadow-card); }
  .dot { flex: none; width: 10px; height: 10px; border-radius: 50%; background: var(--ok); box-shadow: 0 0 0 3px var(--ok-glow); }
  /* Open admission pulses, as in the class page mockup; reduced motion keeps the still halo above (the theme stops animations). */
  .admitting:not(.closed) .dot { animation: admit-pulse 1.8s infinite; }
  @keyframes admit-pulse { 0% { box-shadow: 0 0 0 0 var(--ok-border); } 70% { box-shadow: 0 0 0 8px var(--transparent); } 100% { box-shadow: 0 0 0 0 var(--transparent); } }
  .closed .dot { background: var(--muted-dot); box-shadow: none; }
  .line { display: flex; flex-direction: column; min-width: 0; margin: 0; overflow-wrap: anywhere; }
  .label { color: var(--ok-text); font-size: .7rem; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; }
  .what { color: var(--text-main); font-weight: 600; } .what span { color: var(--text-muted); font-weight: 500; }
  .buttons { display: flex; flex-wrap: wrap; gap: .5rem; margin-left: auto; }
  .buttons button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; margin: 0; min-height: 38px; padding: .3rem 1rem; border-radius: 9px; background: var(--bg-raised); border-color: var(--ok-border); color: var(--ok-text); font-size: .92rem; }
  .buttons button:first-child { background: var(--ok-text); border-color: var(--ok-text); color: var(--bg-primary); }
  .buttons button:hover:not(:disabled) { background: var(--ok-fill); border-color: var(--ok); color: var(--ok-text); }
  .problem { flex: 1 1 100%; margin: 0; }
  /* The working count is a labelled status: muted at zero, bold above it. */
  .count { padding: .1rem .55rem; border: 1px solid var(--ok-border); border-radius: 999px; background: var(--bg-raised); color: var(--ok-text); font-family: var(--font-mono); font-size: .8rem; font-weight: 700; white-space: nowrap; }
  @media (max-width: 760px) { .buttons { margin-left: 0; width: 100%; } }
</style>

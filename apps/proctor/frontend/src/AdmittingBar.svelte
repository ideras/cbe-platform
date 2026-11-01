<script>
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
    <p class="line"><strong>Admitting:</strong> {exam.title} — {catalog.current_class_name} · <span class="count" class:live={exam.active_sessions > 0}>{exam.active_sessions} working</span>{#if lockedCount} · {lockedCount} locked{/if}</p>
    <div class="buttons">
      <button type="button" disabled={locked} title="Open the monitoring window for {exam.title}" onclick={monitor}>Monitor</button>
      <button type="button" disabled={locked} title="Open {catalog.current_class_name}, on its exams" onclick={() => onOpenClass(catalog.current_class_id)}>Open class</button>
    </div>
    {#if problem}<p class="problem" role="alert">{problem}</p>{/if}
  {:else}
    <span class="dot" aria-hidden="true"></span>
    <p class="line">Admission closed</p>
  {/if}
</section>
<style>
  /* Open admission is the mockups' success state; closed is a neutral raised strip with a grey dot. */
  .admitting { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2) var(--space-3); padding: .55rem 1rem; background: var(--ok-fill); border: 1px solid var(--ok-border); border-radius: 10px; color: var(--ok-text); }
  .admitting strong { color: inherit; }
  .admitting.closed { background: var(--bg-raised); border-color: var(--border-subtle); color: var(--text-muted); box-shadow: var(--shadow-card); }
  .dot { flex: none; width: 10px; height: 10px; border-radius: 50%; background: var(--ok); box-shadow: 0 0 0 3px var(--ok-glow); }
  .closed .dot { background: var(--muted-dot); box-shadow: none; }
  .line { flex: 1 1 18rem; min-width: 0; margin: 0; overflow-wrap: anywhere; }
  .buttons { display: flex; gap: var(--space-2); margin-left: auto; }
  .buttons button { margin: 0; min-height: 2.3rem; padding-block: .2rem; }
  .problem { flex: 1 1 100%; margin: 0; }
  /* The working count is a labelled status: muted at zero, bold above it. */
  .count { color: var(--text-muted); }
  .count.live { color: var(--ok-text); font-weight: 700; }
</style>

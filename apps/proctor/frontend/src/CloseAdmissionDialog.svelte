<script>
  import { untrack } from 'svelte';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  // Closing admission: nobody new can log in, and the students already in keep their sessions. The dialog says which exam this is
  // and what its login window is doing now, who is working in it, and how many of its class have not logged in yet. It asks for
  // a plain confirmation and, when students are still working, for an override of its own: if their application closes or crashes
  // they cannot log back in. onConfirm receives { allowActive }. While the server works nothing can be closed.
  // The details are what the proctor saw when they asked: the exam published now (current) with its class (currentClass), the
  // class being looked at (klass), and whether the exam's login window is open (admission). working is how many students are
  // working in the exam as the dashboard last heard it; the count in details is only what the proctor saw when they opened the
  // dialog.
  let { details, working: live = null, busy = false, error = '', onCancel, onConfirm } = $props();
  const exam = untrack(() => details.current);
  const className = untrack(() => details.currentClass);
  let confirmed = $state(false);
  // Closing while students are still working needs its own tick, apart from the plain confirmation.
  let override = $state(false);
  const working = $derived(live ?? exam.active_sessions ?? 0);
  // The override is given for the students working when it is ticked. If the count changes, the label and the warning say the new
  // number and the tick is asked for again.
  let counted = untrack(() => working);
  $effect(() => {
    const now = working;
    if (now !== counted) { counted = now; override = false; }
  });
  const ready = $derived(confirmed && (working === 0 || override));

  // What the exam's login window is doing now. The dialog opens for any published exam, whether or not students can start it.
  const NOW = { open: 'Admitting', 'not open yet': 'Waiting to open', closed: 'Login window closed', ended: 'Exam ended' };
  const nowText = NOW[details.admission] ?? NOW.open;
  const admitting = details.admission === 'open';
  // The students of the class with no session in the exam, from the class's roster and the exam's sessions as they were when the
  // dialog opened. A roster that changed since students took it can leave more sessions than students: then there is no one to say.
  const notLoggedIn = untrack(() => details.klass?.class_id === exam.class_id ? Math.max(0, (details.klass.student_count ?? 0) - (exam.total_sessions ?? 0)) : 0);

  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }
</script>
<Modal title="Close admission" titleId="close-admission-title" subtitle="Stops new logins; running sessions continue" tone="deep" icon="closeAdmission" width="540px" {busy} onClose={onCancel}>
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm({ allowActive: working > 0 && override }); }}>
    <div class="content">
      <div class="status">
        <div class="st now" class:admitting>
          <span class="k">Now</span>
          <span class="v"><span class="dot" class:live={admitting} class:waiting={!admitting}></span>{nowText}</span>
        </div>
        <Icon name="arrowRight" size={18} class="arrow" />
        <div class="st next">
          <span class="k">After</span>
          <span class="v"><span class="dot off"></span>Closed</span>
        </div>
        <div class="exam-name"><Icon name="exams" size={15} /><span><b>{exam.title}</b> · {className}</span></div>
      </div>

      <div class="effects">
        <div class="eff block">
          <span class="mk"><Icon name="ban" size={13} /></span>
          <div><div class="h">New logins blocked</div><div class="s">Nobody new can start this exam.</div></div>
        </div>
        <div class="eff keep">
          <span class="mk"><Icon name="check" size={13} /></span>
          {#if working > 0}
            <div><div class="h">Running sessions continue</div><div class="s">Their sessions and timers are untouched.</div></div>
          {:else}
            <div><div class="h">Nothing running is affected</div><div class="s">Nobody is working in it now.</div></div>
          {/if}
        </div>
      </div>

      {#if notLoggedIn > 0}
        <p class="heads-up"><Icon name="warning" size={16} /><span><strong>{plural(notLoggedIn, 'student')} {notLoggedIn === 1 ? 'hasn’t' : 'haven’t'} logged in yet</strong> and won’t be able to after this.</span></p>
      {/if}
      {#if working > 0}
        <p class="notice warning"><Icon name="warning" size={16} /><span><strong>{plural(working, 'student')} still working in “{exam.title}” ({className}).</strong> They keep their sessions and can keep saving and submitting, but if their application closes or crashes they cannot log back in to it.</span></p>
      {/if}
      <p class="notice info"><Icon name="info" size={16} /><span>To let students in again, publish the exam again.</span></p>

      <div class="acks">
        {#if working > 0}
          <label class="confirm override" class:checked={override}>
            <input type="checkbox" bind:checked={override} disabled={busy} />
            <span class="box"><Icon name="check" size={14} /></span>
            <span><strong>Override:</strong> close admission anyway, although {plural(working, 'student')} {working === 1 ? 'is' : 'are'} still working in “{exam.title}”.</span>
          </label>
        {/if}
        <label class="confirm" class:checked={confirmed}>
          <input type="checkbox" bind:checked={confirmed} disabled={busy} />
          <span class="box"><Icon name="check" size={14} /></span>
          <span>I want to close admission.</span>
        </label>
      </div>

      {#if error}<p class="notice problem" role="alert"><Icon name="error" size={16} /><span>{error}</span></p>{/if}
    </div>

    <div class="foot">
      <button type="button" class="cancel" disabled={busy} onclick={onCancel}>Cancel</button>
      <button type="submit" class="primary" disabled={busy || !ready}><Icon name="lock" size={16} />{busy ? 'Waiting for server…' : 'Close admission'}</button>
    </div>
  </form>
</Modal>
<style>
  p { margin: 0; overflow-wrap: anywhere; }
  .content { display: flex; flex-direction: column; gap: 1rem; padding: 1.1rem; }

  /* Now, then after: what the exam's admission is doing and what it will be. */
  .status { display: flex; flex-wrap: wrap; align-items: center; gap: .7rem; padding: .75rem .9rem; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--bg-primary); }
  .st { display: flex; flex-direction: column; min-width: 0; }
  .k { color: var(--text-muted); font-size: .68rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .v { display: inline-flex; align-items: center; gap: .45rem; font-weight: 700; }
  .now.admitting .k { color: var(--ok-text); }
  .next .v { color: var(--text-muted); }
  .dot { flex: none; width: 10px; height: 10px; border-radius: 50%; }
  .dot.live { background: var(--ok); animation: live-pulse 1.8s infinite; }
  .dot.waiting { background: var(--warn); }
  .dot.off { background: var(--muted-dot); }
  @keyframes live-pulse { 0% { box-shadow: 0 0 0 0 var(--ok-border); } 70% { box-shadow: 0 0 0 7px var(--transparent); } 100% { box-shadow: 0 0 0 0 var(--transparent); } }
  @media (prefers-reduced-motion: reduce) { .dot.live { animation: none; } }
  .status :global(.arrow) { flex: none; color: var(--action-primary); }
  .exam-name { display: flex; align-items: center; gap: .5rem; width: 100%; margin-top: .1rem; padding-top: .6rem; border-top: 1px dashed var(--border-subtle); color: var(--text-muted); font-size: .88rem; overflow-wrap: anywhere; }
  .exam-name :global(svg) { flex: none; color: var(--action-primary); }
  .exam-name b { color: var(--text-main); }

  /* What changes and what does not. */
  .effects { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .6rem; }
  .eff { display: flex; align-items: flex-start; gap: .6rem; min-width: 0; padding: .65rem .75rem; border: 1px solid; border-radius: 10px; font-size: .86rem; }
  .eff .mk { display: grid; place-items: center; flex: none; width: 26px; height: 26px; margin-top: .05rem; border-radius: 50%; }
  .eff .h { font-weight: 700; overflow-wrap: anywhere; }
  .eff .s { font-size: .8rem; overflow-wrap: anywhere; }
  .eff.block { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .eff.block .mk { background: var(--warn-text); color: var(--warn-fill); }
  .eff.keep { border-color: var(--ok-border); background: var(--ok-fill); color: var(--ok-text); }
  .eff.keep .mk { background: var(--ok-text); color: var(--ok-fill); }

  /* Notices: who this affects, what is lost and what went wrong. */
  .notice, .heads-up { display: flex; align-items: flex-start; gap: .55rem; padding: .55rem .7rem; border: 1px solid var(--action-border); border-left: 4px solid var(--action-primary); border-radius: 9px; background: var(--hover-fill); color: var(--action-hover); font-size: .86rem; font-weight: 400; }
  .notice :global(svg), .heads-up :global(svg) { flex: none; margin-top: .15rem; }
  .notice strong, .heads-up strong { color: inherit; }
  .notice.warning { border-color: var(--warn-border); border-left-color: var(--warn); background: var(--warn-fill); color: var(--warn-text); }
  .heads-up, .notice.problem { border-color: var(--alert-border); border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }

  /* The ticks. The real checkbox stays in the label, invisible, so the keyboard and screen readers keep a control; .box is what is drawn. */
  .acks { display: flex; flex-direction: column; gap: .5rem; }
  .confirm { position: relative; display: flex; align-items: center; gap: .65rem; margin: 0; padding: .65rem .8rem; border: 1px solid var(--border-subtle); border-left: 4px solid var(--warn); border-radius: 9px; background: var(--bg-primary); font-size: .92rem; font-weight: 600; cursor: pointer; user-select: none; transition: background-color .2s, border-color .2s; }
  .confirm:hover { background: var(--hover-fill); }
  .confirm input { position: absolute; width: 20px; height: 20px; margin: 0; opacity: 0; pointer-events: none; }
  .box { display: grid; place-items: center; flex: none; width: 20px; height: 20px; border: 2px solid var(--text-muted); border-radius: 5px; color: var(--transparent); transition: background-color .15s, border-color .15s, color .15s; }
  .confirm:has(input:focus-visible) .box { box-shadow: 0 0 0 3px var(--focus-halo-strong); }
  .confirm.checked { border-left-color: var(--ok); background: var(--ok-fill); color: var(--ok-text); }
  .confirm.checked .box { border-color: var(--ok-text); background: var(--ok-text); color: var(--ok-fill); }
  /* The override is a warning until it is ticked, so it reads apart from the plain confirmation below it. */
  .confirm.override:not(.checked) { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .confirm.override:not(.checked):hover { border-color: var(--warn); }
  .confirm.override:not(.checked) .box { border-color: var(--warn-text); }
  .confirm strong { color: inherit; }

  /* Footer: it stays at the bottom of the dialog while the content scrolls. */
  .foot { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .foot button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; white-space: nowrap; }
  .cancel { background: var(--transparent); color: var(--text-muted); }
  .cancel:hover:not(:disabled) { background: var(--bg-secondary); border-color: var(--border-subtle); color: var(--text-main); }

  @media (max-width: 520px) {
    .effects { grid-template-columns: minmax(0, 1fr); }
    .foot { flex-direction: column-reverse; }
    .foot button { width: 100%; }
  }
</style>

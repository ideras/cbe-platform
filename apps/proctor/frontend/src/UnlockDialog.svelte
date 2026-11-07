<script>
  import { onMount, untrack } from 'svelte';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { parseTime } from './dates.js';
  import { initials, clockDuration, unlockChecks } from './roster.js';
  // Unlocking a student's exam. The dialog says who is locked and why, shows whether the student looks ready (the application reaches
  // the server, the student is in the exam window) and asks for the reason, which is always recorded, and for the exam time. The time
  // given back is the proctor's explicit choice: without it no time is added.
  // student is the row the proctor chose; live is the same student in the roster as it is now (or null), so that the checks follow
  // the heartbeats while the dialog is open, and serverTime is the server's clock as the roster last reported it. The times here are
  // the server's, which the unlock itself uses, and keep counting from it between reports.
  let { student, live = null, serverTime = '', exam = null, busy = false, error = '', onReview, onCancel, onConfirm } = $props();
  const REASONS = ['Network change', 'Computer restarted', 'App crashed', 'Left by mistake'];

  let reason = $state('');
  let time = $state('keep');
  let problem = $state('');
  let opening = $state(false);
  let reasonField = $state(null);

  // Her row as it is now. The lock is kept as it was when the dialog opened if the roster no longer has one, to say it is gone.
  const opened = untrack(() => student.locked);
  const row = $derived(live ?? student);
  const lock = $derived(row.locked ?? opened);
  const stillLocked = $derived(!!row.locked);
  const checks = $derived(unlockChecks(row));
  const ready = $derived(reason.trim().length > 0 && stillLocked);

  // The server's clock: its last report, and what this computer's clock has counted since.
  let anchor = $state({ server: untrack(() => parseTime(serverTime)?.getTime()) ?? Date.now(), local: Date.now() });
  let tickAt = $state(Date.now());
  $effect(() => {
    const server = parseTime(serverTime)?.getTime();
    if (server != null) untrack(() => { anchor = { server, local: Date.now() }; });
  });
  const serverNow = $derived(anchor.server + Math.max(0, tickAt - anchor.local));
  const lockedMs = $derived.by(() => {
    const since = parseTime(lock?.since)?.getTime();
    return since == null ? 0 : Math.max(0, serverNow - since);
  });
  const deadline = $derived(parseTime(row.deadline_at)?.getTime() ?? null);
  const endsAt = ms => ms == null ? 'not reported' : new Date(ms).toLocaleTimeString();
  const left = ms => ms == null ? 'unknown' : clockDuration(ms);

  onMount(() => {
    // The dialog is open once Modal has mounted, which is before this component's own mount.
    reasonField?.focus();
    const timer = setInterval(() => tickAt = Date.now(), 1000);
    return () => clearInterval(timer);
  });

  async function review() {
    if (opening) return;
    opening = true; problem = '';
    try { await onReview(row); } catch (err) { problem = String(err); } finally { opening = false; }
  }
</script>
<Modal title="Unlock exam" titleId="unlock-title" subtitle={exam ? `${exam.title} · ${exam.class_name}` : ''} tone="deep" icon="lockOpen" width="540px" {busy} onClose={onCancel}>
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm({ reason: reason.trim(), giveBack: time === 'give' }); }}>
    <div class="content">
      {#if !stillLocked}
        <p class="notice warning" role="status"><Icon name="warning" size={16} /><span>This exam is no longer locked.</span></p>
      {/if}

      <div class="student">
        <div class="student-top">
          <span class="avatar" aria-hidden="true">{initials(row.student_name)}</span>
          <div>
            <div class="name">{row.student_name}</div>
            <div class="acct">{row.account_number}</div>
          </div>
          <span class="locked-badge"><Icon name="lock" size={12} />{stillLocked ? 'Locked' : 'Not locked'}</span>
        </div>
        <div class="why">
          {#if lock?.reason === 'network'}
            <Icon name="wifi" size={15} /><span>The student locked it <b>to change network</b></span>
          {:else if lock?.reason === 'no_contact'}
            <Icon name="wifiOff" size={15} /><span>The app <b>could not reach the server</b></span>
          {:else}
            <Icon name="eyeOff" size={15} /><span>The student was <b>away from the exam window</b></span>
          {/if}
          <span class="ago">{clockDuration(lockedMs)} ago</span>
        </div>
      </div>

      {#if lock?.reason === 'network' || lock?.reason === 'no_contact'}
        <p class="while"><Icon name="screen" size={16} /><span>While locked, the alarm stayed quiet and the student could use the computer to reconnect. Their screens were captured.{#if onReview}{' '}<button type="button" class="link" disabled={opening} onclick={review}>Review screenshots</button>{/if}</span></p>
      {/if}

      <div>
        <div class="section-label">Ready to unlock? <span>· live from the roster</span></div>
        <div class="checks" aria-live="polite">
          <div class="chk" class:yes={checks.network} class:no={!checks.network}>
            <span class="mk"><Icon name={checks.network ? 'check' : 'clock'} size={13} /></span>
            <div><div class="h">Exam network</div><div class="s">{checks.network ? 'Connected · heartbeat ok' : 'Not reachable yet'}</div></div>
          </div>
          <div class="chk" class:yes={checks.window} class:no={!checks.window}>
            <span class="mk"><Icon name={checks.window ? 'check' : 'clock'} size={13} /></span>
            <div><div class="h">Exam window</div><div class="s">{checks.window ? 'Back in the exam window' : 'Waiting for the student'}</div></div>
          </div>
        </div>
      </div>

      <div>
        <label class="section-label" for="unlock-reason">Reason <span>· saved in the audit log</span></label>
        <div class="reason-wrap">
          <input id="unlock-reason" type="text" bind:this={reasonField} bind:value={reason} required maxlength="1024" autocomplete="off" placeholder="Why are you unlocking it?" disabled={busy} />
        </div>
        <div class="chips">
          {#each REASONS as option (option)}
            <button type="button" class="chip" class:on={reason.trim() === option} aria-pressed={reason.trim() === option} disabled={busy} onclick={() => reason = option}>{option}</button>
          {/each}
        </div>
      </div>

      <div>
        <div class="section-label">Exam time</div>
        <div class="times" role="radiogroup" aria-label="Exam time">
          <label class="opt">
            <input type="radio" name="time" value="keep" bind:group={time} disabled={busy} aria-label="Keep the deadline" aria-describedby="keep-ends keep-left" />
            <span class="top">Keep the deadline<span class="radio"></span></span>
            <span class="dl" id="keep-ends">Ends <b>{endsAt(deadline)}</b></span>
            <span class="left" id="keep-left"><b>{left(deadline == null ? null : deadline - serverNow)}</b> left</span>
          </label>
          <label class="opt">
            <input type="radio" name="time" value="give" bind:group={time} disabled={busy} aria-label="Give back the time the exam was locked" aria-describedby="give-ends give-left" />
            <span class="top">Give back <span class="plus">+{clockDuration(lockedMs)}</span><span class="radio"></span></span>
            <span class="dl" id="give-ends">Ends <b>{endsAt(deadline == null ? null : deadline + lockedMs)}</b></span>
            <span class="left" id="give-left"><b>{left(deadline == null ? null : deadline + lockedMs - serverNow)}</b> left</span>
          </label>
        </div>
        <p class="time-note">The deadline doesn’t move while the exam is locked; the clock kept running.</p>
      </div>

      {#if error}<p class="notice problem" role="alert"><Icon name="error" size={16} /><span>{error}</span></p>{/if}
      {#if problem}<p class="notice problem" role="alert"><Icon name="error" size={16} /><span>{problem}</span></p>{/if}
    </div>

    <div class="foot">
      {#if stillLocked && !(checks.network && checks.window)}<span class="foot-note">Student isn’t ready yet</span>{/if}
      <button type="button" class="cancel" disabled={busy} onclick={onCancel}>Cancel</button>
      <button type="submit" class="primary" disabled={busy || !ready}><Icon name="lockOpen" size={16} />{busy ? 'Unlocking…' : time === 'give' ? `Unlock · +${clockDuration(lockedMs)}` : 'Unlock'}</button>
    </div>
  </form>
</Modal>
<style>
  p { margin: 0; overflow-wrap: anywhere; }
  .content { display: flex; flex-direction: column; gap: 1rem; padding: 1.1rem; }
  .section-label { display: block; margin: 0 0 .45rem; color: var(--action-primary); font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .section-label span { color: var(--text-muted); font-weight: 500; letter-spacing: 0; text-transform: none; }

  /* Who is locked, and why. */
  .student { overflow: hidden; border: 1px solid var(--alert-border); border-left: 4px solid var(--alert); border-radius: 12px; background: var(--bg-primary); }
  .student-top { display: flex; flex-wrap: wrap; align-items: center; gap: .75rem; padding: .75rem .9rem; }
  .avatar { display: grid; place-items: center; flex: none; width: 40px; height: 40px; border-radius: 50%; background: var(--bg-secondary); color: var(--text-main); font-size: .85rem; font-weight: 700; }
  .name { font-size: 1.02rem; font-weight: 700; overflow-wrap: anywhere; }
  .acct { color: var(--action-hover); font-family: var(--font-mono); font-size: .82rem; }
  .locked-badge { display: inline-flex; align-items: center; gap: .35rem; margin-left: auto; padding: .18rem .6rem; border-radius: 999px; background: var(--alert-fill); color: var(--alert-text); font-size: .75rem; font-weight: 700; letter-spacing: .04em; text-transform: uppercase; white-space: nowrap; }
  .why { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; padding: .6rem .9rem; border-top: 1px dashed var(--border-subtle); color: var(--text-muted); font-size: .88rem; }
  .why :global(svg) { flex: none; color: var(--alert); }
  .why b { color: var(--text-main); }
  .ago { margin-left: auto; padding: .08rem .5rem; border-radius: 999px; background: var(--alert-fill); color: var(--alert-text); font-family: var(--font-mono); font-size: .8rem; font-weight: 700; }

  /* Notices: what happened while it was locked, and what went wrong. */
  .while, .notice { display: flex; align-items: flex-start; gap: .55rem; padding: .55rem .75rem; border: 1px solid var(--warn-border); border-left: 4px solid var(--warn); border-radius: 9px; background: var(--warn-fill); color: var(--warn-text); font-size: .85rem; }
  .while :global(svg), .notice :global(svg) { flex: none; margin-top: .15rem; }
  .notice.problem { border-color: var(--alert-border); border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }
  .link { display: inline; min-height: 0; margin: 0; padding: 0; border: 0; background: var(--transparent); color: var(--action-hover); font: inherit; font-weight: 700; text-decoration: underline; cursor: pointer; box-shadow: none; }
  .link:hover:not(:disabled) { background: var(--transparent); color: var(--action-primary); }

  /* The two things to wait for. */
  .checks { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .6rem; }
  .chk { display: flex; align-items: center; gap: .6rem; min-width: 0; padding: .6rem .75rem; border: 1px solid; border-radius: 10px; font-size: .86rem; transition: background-color .2s, border-color .2s; }
  .chk .mk { display: grid; place-items: center; flex: none; width: 26px; height: 26px; border-radius: 50%; }
  .chk .h { font-weight: 700; }
  .chk .s { font-size: .78rem; overflow-wrap: anywhere; }
  .chk.yes { border-color: var(--ok-border); background: var(--ok-fill); color: var(--ok-text); }
  .chk.yes .mk { background: var(--ok-text); color: var(--ok-fill); }
  .chk.no { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .chk.no .mk { background: var(--warn-text); color: var(--warn-fill); }

  /* The reason, and the usual ones. */
  .reason-wrap { display: flex; align-items: center; height: 42px; padding: 0 .75rem; border: 1px solid var(--border-subtle); border-radius: 9px; background: var(--bg-primary); transition: border-color .2s, box-shadow .2s, background-color .2s; }
  .reason-wrap:focus-within { border-color: var(--action-primary); background: var(--paper); box-shadow: 0 0 0 3px var(--focus-halo); }
  .reason-wrap input { flex: 1; min-width: 0; height: auto; padding: 0; border: 0; outline: 0; background: var(--transparent); box-shadow: none; font: inherit; font-size: .95rem; }
  /* The wrapper shows the focus, so the field inside does not draw it a second time. */
  .reason-wrap input:focus-visible { border-color: var(--transparent); background: var(--transparent); box-shadow: none; }
  .chips { display: flex; flex-wrap: wrap; gap: .35rem; margin-top: .5rem; }
  .chip { min-height: 0; margin: 0; padding: .22rem .65rem; border-radius: 999px; background: var(--bg-primary); color: var(--text-muted); font-size: .8rem; font-weight: 400; }
  .chip:hover:not(:disabled) { border-color: var(--accent-cyan-soft); background: var(--bg-primary); color: var(--action-hover); }
  .chip.on, .chip.on:hover:not(:disabled) { border-color: var(--action-primary); background: var(--selection-fill); color: var(--action-hover); font-weight: 700; }

  /* The exam time: keep the deadline, or give back what the lock lasted. The real radio stays in the label, invisible. */
  .times { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .6rem; }
  .opt { position: relative; display: flex; flex-direction: column; gap: .2rem; min-width: 0; padding: .7rem .8rem; border: 1.5px solid var(--border-subtle); border-radius: 12px; background: var(--bg-primary); cursor: pointer; transition: border-color .15s, background-color .15s, box-shadow .15s; }
  .opt:hover { border-color: var(--accent-cyan-soft); background: var(--hover-fill); }
  .opt input { position: absolute; width: 18px; height: 18px; margin: 0; opacity: 0; pointer-events: none; }
  .top { display: flex; align-items: center; gap: .5rem; font-size: .9rem; font-weight: 700; }
  .radio { display: grid; place-items: center; width: 18px; height: 18px; margin-left: auto; border: 2px solid var(--text-muted); border-radius: 50%; }
  .radio::after { content: ''; width: 8px; height: 8px; border-radius: 50%; }
  .dl, .left { font-size: .78rem; color: var(--text-muted); }
  .dl b, .left b { font-family: var(--font-mono); color: var(--text-main); }
  .plus { padding: .02rem .4rem; border-radius: 5px; background: var(--warn-fill); color: var(--warn-text); font-family: var(--font-mono); font-size: .75rem; font-weight: 700; }
  .opt:has(input:checked) { border-color: var(--action-primary); background: var(--selection-fill); box-shadow: var(--shadow-card-hover); }
  .opt:has(input:checked) .dl, .opt:has(input:checked) .left { color: var(--text-main); }
  .opt:has(input:checked) .radio { border-color: var(--action-primary); }
  .opt:has(input:checked) .radio::after { background: var(--action-primary); }
  .opt:has(input:focus-visible) { box-shadow: 0 0 0 3px var(--focus-halo-strong); }
  .time-note { margin-top: .45rem; color: var(--text-muted); font-size: .78rem; }

  /* Footer: it stays at the bottom of the dialog while the content scrolls. */
  .foot { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .foot button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; white-space: nowrap; }
  .foot-note { margin-right: auto; color: var(--warn-text); font-size: .8rem; }
  .cancel { background: var(--transparent); color: var(--text-muted); }
  .cancel:hover:not(:disabled) { background: var(--bg-secondary); border-color: var(--border-subtle); color: var(--text-main); }

  @media (max-width: 520px) {
    .checks, .times { grid-template-columns: minmax(0, 1fr); }
    .locked-badge, .ago { margin-left: 0; }
    .foot-note { display: none; }
    .foot { flex-direction: column-reverse; }
    .foot button { width: 100%; }
  }
</style>

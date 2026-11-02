<script>
  import { onMount, tick, untrack } from 'svelte';
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { formatTime } from '../../../../shared/exam/timing.js';
  import { parseTime } from './dates.js';
  import { suggestedEndTime, endTimeLabel, endTimeETA } from './end-time.js';
  // Publishing an exam admits the students of its class. The dialog says who can log in, asks for the optional end time, and
  // unlocks its button only after the proctor ticks that they want this exam published. It stays open afterwards: the result is
  // the PIN to read out to the class and a way to the live roster. The details are what the proctor saw when they asked: the
  // exam (entry), its class (klass), the exam published now (current) with its class (currentClass), and whether the exam's
  // login window is open. While the server works nothing can be closed.
  // The PIN is not in the exam list. It is read from the server when the dialog opens, is shown masked until the proctor reveals
  // or copies it, and lives only in this dialog.
  // working is how many students are working in the exam published now, as the dashboard last heard it; the count in details is only
  // what the proctor saw when they opened the dialog.
  let { details, working: live = null, onClose, onPublished, onFailed, onMonitor } = $props();
  const exam = untrack(() => details.entry);
  const klass = untrack(() => details.klass);
  const current = untrack(() => details.current);
  const MASK = '••••••';

  // An exam that already has an end time offers it again; one that has none starts without.
  let mode = $state(exam.ends_at ? 'set' : 'none');
  let value = $state(suggestedEndTime(exam.ends_at));
  let confirmed = $state(false);
  // Replacing an exam whose students are still working needs its own tick, apart from the plain confirmation.
  let override = $state(false);
  // form, publishing or done.
  let stage = $state('form');
  let problem = $state('');
  let preview = $state(null);
  let previewProblem = $state('');
  let now = $state(Date.now());
  // The end time as it was published, for the result.
  let endedAt = $state('');
  let pin = $state(null);
  let pinProblem = $state('');
  let revealed = $state(false);
  let toast = $state('');
  let toastTimer;
  let holder = $state(null);
  let rosterButton = $state(null);
  let opening = $state(false);

  const busy = $derived(stage === 'publishing');
  const done = $derived(stage === 'done');
  const working = $derived(live ?? current?.active_sessions ?? 0);
  // The override is given for the students working when it is ticked. If the count changes, the label and the warning say the new
  // number and the tick is asked for again.
  let counted = untrack(() => working);
  $effect(() => {
    const now = working;
    if (now !== counted) { counted = now; override = false; }
  });
  const students = klass.student_count;
  const durationMinutes = Number((exam.duration_seconds / 60).toFixed(1));
  const proposed = $derived(mode === 'set' ? parseTime(preview?.ends_at) : null);
  const minutes = $derived(proposed ? Math.ceil((proposed.getTime() - now) / 60000) : null);
  // The server takes an end at least a minute and at most a day away; a time that has passed today means tomorrow.
  const tooSoon = $derived(!!proposed && proposed.getTime() - now < 60000);
  const short = $derived(!!proposed && !tooSoon && minutes < durationMinutes);
  const tomorrow = $derived(!!proposed && proposed.toDateString() !== new Date(now).toDateString());
  const eta = $derived(proposed ? endTimeETA(proposed, now) : '');
  const hint = $derived.by(() => {
    if (mode === 'none') return { text: `Each student gets the full ${durationMinutes} min (plus grace) from their first login.`, warn: false };
    if (!proposed) return { text: '', warn: false };
    if (tooSoon) return { text: 'Choose an end time at least one minute from now.', warn: true };
    if (short) return { text: `Students who start now get only ${minutes} of their ${durationMinutes} min.`, warn: true };
    const latest = new Date(proposed.getTime() - exam.duration_seconds * 1000).toISOString();
    return { text: `Latest login that still gets the full ${durationMinutes} min: ${endTimeLabel(latest)}.`, warn: false };
  });
  const beforeOpening = $derived(!!proposed && !!exam.opens_at && proposed <= parseTime(exam.opens_at));
  const afterClosing = $derived(!!proposed && !!exam.closes_at && proposed > parseTime(exam.closes_at));
  const timeOk = $derived(mode === 'none' || (!!value && !!proposed && !previewProblem && !tooSoon));
  const overridden = $derived(working > 0 && override);
  const ready = $derived(confirmed && (working === 0 || override) && timeOk && stage === 'form');

  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }

  // The end time is resolved by the dashboard, which knows the next occurrence of a clock time and whether this computer and the
  // server agree on the time.
  $effect(() => {
    const time = mode === 'set' ? value : '';
    let live = true;
    preview = null; previewProblem = '';
    if (time) api.PreviewEndTime(time).then(result => { if (live) preview = result; }).catch(err => { if (live) previewProblem = String(err); });
    return () => { live = false; };
  });
  onMount(() => {
    const timer = setInterval(() => now = Date.now(), 30000);
    readPin().catch(() => {}); // a failure is reported when the proctor asks for the PIN
    return () => { clearInterval(timer); clearTimeout(toastTimer); };
  });

  // Any change to when the exam ends is a new decision, so the tick is asked for again.
  function change(action) { action(); confirmed = false; }

  // The PIN, read once per dialog.
  async function readPin() {
    if (pin === null) pin = await api.ExamPIN(exam.exam_id);
    return pin;
  }
  // The PIN for something the proctor asked for, or null after saying why it could not be read.
  async function ask() {
    try {
      const text = await readPin();
      pinProblem = '';
      return text;
    } catch (err) {
      pinProblem = `The PIN could not be read: ${String(err)}`;
      return null;
    }
  }
  async function reveal() {
    if (revealed) { revealed = false; return; }
    if (await ask() !== null) revealed = true;
  }
  function say(text) {
    toast = text;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toast = '', 1400);
  }
  // The clipboard API where the webview offers it; otherwise the selection of a hidden field inside the dialog, since nothing
  // outside a modal dialog can take focus.
  async function write(text) {
    try { await navigator.clipboard.writeText(text); return true; } catch {}
    const field = document.createElement('textarea');
    field.value = text; field.readOnly = true; field.style.position = 'fixed'; field.style.opacity = '0';
    holder.append(field);
    field.select();
    let copied = false;
    try { copied = document.execCommand('copy'); } catch {}
    field.remove();
    return copied;
  }
  async function copy() {
    const text = await ask();
    if (text !== null) say(await write(text) ? 'PIN copied' : 'Could not copy the PIN. Reveal it and copy it by hand.');
  }

  async function publish() {
    if (!ready) return;
    problem = '';
    stage = 'publishing';
    try {
      const expected = current?.exam_id ?? '';
      if (mode === 'set') {
        await api.PublishTimed(exam.exam_id, expected, overridden, true, value);
      } else {
        // "No end time" must be true: an end time the exam kept from an earlier publish is cleared before students are admitted.
        if (exam.ends_at) await api.SetEndTime(exam.exam_id, '', true);
        await api.Publish(exam.exam_id, expected, overridden, true);
      }
    } catch (err) {
      // The server may have done part of it before failing, so the list is read again; the tick stays as the proctor left it. If the
      // refusal was students who started working after the dialog opened, the new count brings in the override tick.
      problem = String(err);
      stage = 'form';
      onFailed?.();
      return;
    }
    endedAt = mode === 'set' ? endTimeLabel(preview?.ends_at) : 'No end time';
    stage = 'done';
    onPublished?.();
    if (pin === null) ask();
    await tick();
    rosterButton?.focus();
  }
  async function openRoster() {
    if (opening) return;
    opening = true; problem = '';
    try { await onMonitor(); } catch (err) { problem = String(err); } finally { opening = false; }
  }
</script>
<Modal title="Publish exam" titleId="publish-title" subtitle="Students can log in as soon as you publish" tone="deep" icon="broadcast" width="580px" {busy} {onClose}>
  <form onsubmit={event => { event.preventDefault(); publish(); }}>
    <div class="content" bind:this={holder}>
      {#if done}
        <div class="hero" role="status">
          <span class="live-dot" aria-hidden="true"></span>
          <div>
            <div class="lbl">Now admitting</div>
            <h3>{exam.title}</h3>
          </div>
        </div>

        <div class="big-pin">
          <div class="big-text">
            <div class="k">Login PIN for students</div>
            {#if pin !== null}
              <code class:long={pin.length > 12}>{pin}</code>
              <div class="s">Share it with {klass.name} only</div>
            {:else if pinProblem}
              <div class="s">Not shown: the PIN could not be read.</div>
            {:else}
              <div class="s">Reading the PIN…</div>
            {/if}
          </div>
          <button type="button" class="light" disabled={pin === null && !pinProblem} onclick={pin === null ? ask : copy}>
            <Icon name="copy" size={15} />{pin === null && pinProblem ? 'Try again' : 'Copy PIN'}
          </button>
        </div>

        <dl class="done-facts">
          <dt>Students</dt><dd>{students} of {klass.name}</dd>
          <dt>Ends</dt><dd>{endedAt}</dd>
        </dl>
      {:else}
        <div class="exam">
          <div class="exam-top">
            <span class="ico"><Icon name="exams" size={17} /></span>
            <div>
              <div class="t">{exam.title}</div>
              <div class="c">{klass.name}</div>
            </div>
          </div>
          <div class="facts">
            <div class="fact"><div class="k">Exercises</div><div class="v mono">{exam.exercise_count}</div></div>
            <div class="fact"><div class="k">Duration</div><div class="v"><span class="time-chip">{formatTime(exam.duration_seconds)}</span></div></div>
            <div class="fact">
              <div class="k">Login PIN</div>
              <div class="v pin">
                <code>{revealed && pin !== null ? pin : MASK}</code>
                <button type="button" class="mini" aria-label={revealed ? 'Hide PIN' : 'Show PIN'} aria-pressed={revealed} title={revealed ? 'Hide PIN' : 'Show PIN'} disabled={busy} onclick={reveal}>
                  <Icon name={revealed ? 'eyeOff' : 'eye'} size={14} />
                </button>
                <button type="button" class="mini" aria-label="Copy PIN" title="Copy PIN" disabled={busy} onclick={copy}><Icon name="copy" size={14} /></button>
              </div>
            </div>
          </div>
        </div>

        <div>
          <div class="section-label">Who can log in · starting now</div>
          <div class="aud">
            <div class="a in"><span class="mark"><Icon name="check" size={13} /></span><span><b>{students}</b> {students === 1 ? 'student' : 'students'} of {klass.name}</span></div>
            <div class="a out"><span class="mark"><Icon name="close" size={13} /></span><span>Students of other classes</span></div>
          </div>
          {#if details.admission !== 'open'}
            <p class="notice warning"><Icon name="warning" size={16} /><span>Its login window is {details.admission} by the server’s clock, so students cannot start it yet.</span></p>
          {/if}
          {#if working > 0}
            <p class="notice warning"><Icon name="warning" size={16} /><span><strong>{plural(working, 'student')} still working in “{current.title}” ({details.currentClass}), the exam published now.</strong> Publishing this exam replaces it: those students keep their sessions and can keep saving and submitting, but if their application closes or crashes they cannot log back in to it.</span></p>
          {:else if current}
            <p class="notice info"><Icon name="info" size={16} /><span>Publishing replaces “{current.title}” ({details.currentClass}), the exam published now. It stops admitting students; nobody is working in it, so no one is affected.</span></p>
          {/if}
        </div>

        <div>
          <div class="section-label">Exam ends <span class="optional">· optional, local time</span></div>
          <div class="segmented" role="group" aria-label="End time mode">
            <button type="button" class:on={mode === 'none'} aria-pressed={mode === 'none'} disabled={busy} onclick={() => change(() => mode = 'none')}>No end time</button>
            <button type="button" class:on={mode === 'set'} aria-pressed={mode === 'set'} disabled={busy} onclick={() => change(() => mode = 'set')}>At a set time</button>
          </div>
          {#if mode === 'set'}
            <div class="end-row">
              <input class="time-input" type="time" bind:value required disabled={busy} aria-label="Exam ends at (local time)" oninput={() => confirmed = false} />
              {#if eta}<span class="eta" title={preview?.label}>{tomorrow ? 'tomorrow · ' : ''}{eta}</span>{/if}
            </div>
          {/if}
          {#if hint.text}<p class="end-hint" class:warn={hint.warn}>{hint.text}</p>{/if}
          {#if preview?.clock_warning}<p class="notice warning"><Icon name="warning" size={16} /><span>The server and this computer's clocks differ by more than one minute. Check the end time carefully.</span></p>{/if}
          {#if beforeOpening}<p class="notice warning"><Icon name="warning" size={16} /><span>This is before admission opens: nobody can take the exam.</span></p>{/if}
          {#if afterClosing}<p class="notice info"><Icon name="info" size={16} /><span>This is after admission closes. Running sessions can continue until the end time.</span></p>{/if}
          {#if previewProblem}<p class="notice problem" role="alert"><Icon name="error" size={16} /><span>{previewProblem}</span></p>{/if}
        </div>

        <div class="acks">
          {#if working > 0}
            <label class="confirm override" class:checked={override}>
              <input type="checkbox" bind:checked={override} disabled={busy} />
              <span class="box"><Icon name="check" size={14} /></span>
              <span><strong>Override:</strong> publish anyway, although {plural(working, 'student')} {working === 1 ? 'is' : 'are'} still working in “{current.title}”.</span>
            </label>
          {/if}
          <label class="confirm" class:checked={confirmed}>
            <input type="checkbox" bind:checked={confirmed} disabled={busy} />
            <span class="box"><Icon name="check" size={14} /></span>
            <span>I want to publish “{exam.title}”.</span>
          </label>
        </div>
      {/if}

      {#if pinProblem}<p class="notice problem" role="alert"><Icon name="error" size={16} /><span>{pinProblem}</span></p>{/if}
      {#if problem}<p class="notice problem" role="alert"><Icon name="error" size={16} /><span>{problem}</span></p>{/if}
    </div>

    <div class="foot">
      {#if done}
        <button type="button" class="close-btn" onclick={onClose}>Close</button>
        <button type="button" class="primary" bind:this={rosterButton} disabled={opening} onclick={openRoster}><Icon name="monitor" size={16} />Open live roster</button>
      {:else}
        <button type="button" class="cancel" disabled={busy} onclick={onClose}>Cancel</button>
        <button type="submit" class="primary" disabled={!ready}><Icon name="broadcast" size={16} />{busy ? 'Publishing…' : 'Publish & admit students'}</button>
      {/if}
    </div>
    <div class="toast" class:show={toast} aria-live="polite">{toast}</div>
  </form>
</Modal>
<style>
  p { margin: 0; overflow-wrap: anywhere; }
  h3 { margin: 0; }
  .content { display: flex; flex-direction: column; gap: 1rem; padding: 1.1rem; }
  .section-label { margin-bottom: .45rem; color: var(--action-primary); font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .optional { color: var(--text-muted); font-weight: 500; letter-spacing: 0; text-transform: none; }
  .mono { font-family: var(--font-mono); }

  /* The exam being published, with what students will need. */
  .exam { overflow: hidden; border: 1px solid var(--action-border); border-radius: 12px; background: var(--bg-primary); }
  .exam-top { display: flex; align-items: center; gap: .75rem; padding: .75rem .9rem; border-bottom: 1px solid var(--cyan-border); background: var(--header-soft); }
  .ico { display: grid; place-items: center; flex: none; width: 34px; height: 34px; border: 1px solid var(--cyan-border); border-radius: 9px; background: var(--bg-raised); color: var(--action-primary); }
  .t { color: var(--action-hover); font-weight: 700; overflow-wrap: anywhere; }
  .c { color: var(--text-main); font-size: .82rem; overflow-wrap: anywhere; }
  .facts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .fact { min-width: 0; padding: .6rem .9rem; border-right: 1px solid var(--border-subtle); }
  .fact:last-child { border-right: 0; }
  .k { color: var(--text-muted); font-size: .68rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .v { margin-top: .1rem; font-weight: 700; }
  .time-chip { display: inline-block; padding: .02rem .4rem; border-radius: 5px; background: var(--warn-fill); color: var(--warn-text); font-family: var(--font-mono); font-weight: 700; }
  .pin { display: flex; flex-wrap: wrap; align-items: center; gap: .35rem; }
  .pin code { min-width: 0; color: var(--action-hover); font-weight: 700; letter-spacing: .12em; overflow-wrap: anywhere; }
  .mini { display: grid; place-items: center; width: 26px; min-height: 26px; height: 26px; padding: 0; border-radius: 6px; background: var(--bg-raised); color: var(--text-muted); }

  /* Who gets in: the class of the exam, and nobody else. */
  .aud { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .6rem; }
  .a { display: flex; align-items: center; gap: .6rem; min-width: 0; padding: .6rem .75rem; border: 1px solid var(--border-subtle); border-radius: 10px; font-size: .86rem; overflow-wrap: anywhere; }
  .a b { font-family: var(--font-mono); }
  .mark { display: grid; place-items: center; flex: none; width: 26px; height: 26px; border-radius: 50%; }
  .a.in { border-color: var(--ok-border); background: var(--ok-fill); color: var(--ok-text); }
  .a.in .mark { background: var(--ok-text); color: var(--ok-fill); }
  .a.out { background: var(--bg-primary); color: var(--text-muted); }
  .a.out .mark { background: var(--text-muted); color: var(--bg-primary); }

  /* When the exam ends. */
  .segmented { display: inline-flex; flex-wrap: wrap; gap: 3px; padding: 3px; border-radius: 10px; background: var(--bg-secondary); }
  .segmented button { min-height: 0; padding: .38rem .85rem; border: 0; border-radius: 8px; background: var(--transparent); color: var(--text-main); font-size: .86rem; }
  .segmented button:hover:not(:disabled) { background: var(--transparent); color: var(--action-hover); }
  .segmented button.on, .segmented button.on:hover:not(:disabled) { background: var(--bg-raised); color: var(--action-hover); box-shadow: var(--shadow-card); }
  .end-row { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; margin-top: .6rem; }
  .time-input { height: 40px; min-width: 0; padding: 0 .7rem; border-radius: 9px; font-family: var(--font-mono); font-size: 1rem; font-weight: 700; }
  .time-input::-webkit-calendar-picker-indicator { cursor: pointer; opacity: .6; }
  .eta { padding: .12rem .55rem; border-radius: 999px; background: var(--warn-fill); color: var(--warn-text); font-family: var(--font-mono); font-size: .8rem; }
  .end-hint { margin-top: .45rem; color: var(--text-muted); font-size: .8rem; }
  .end-hint.warn { color: var(--alert-text); }

  /* Notices: what else this publish does, and what went wrong. */
  .notice { display: flex; align-items: flex-start; gap: .55rem; margin-top: .6rem; padding: .55rem .7rem; border: 1px solid var(--action-border); border-left: 4px solid var(--action-primary); border-radius: 9px; background: var(--hover-fill); color: var(--action-hover); font-size: .86rem; font-weight: 400; }
  .notice :global(svg) { margin-top: .15rem; }
  .notice strong { color: inherit; }
  .notice.warning { border-color: var(--warn-border); border-left-color: var(--warn); background: var(--warn-fill); color: var(--warn-text); }
  .notice.problem { border-color: var(--alert-border); border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }
  .content > .notice { margin-top: 0; }

  /* The tick. The real checkbox stays in the label, invisible, so the keyboard and screen readers keep a control; .box is what is drawn. */
  .confirm { position: relative; display: flex; align-items: center; gap: .65rem; margin: 0; padding: .65rem .8rem; border: 1px solid var(--border-subtle); border-left: 4px solid var(--warn); border-radius: 9px; background: var(--bg-primary); font-size: .92rem; font-weight: 600; cursor: pointer; user-select: none; transition: background-color .2s, border-color .2s; }
  .confirm:hover { background: var(--hover-fill); }
  .confirm input { position: absolute; width: 20px; height: 20px; margin: 0; opacity: 0; pointer-events: none; }
  .box { display: grid; place-items: center; flex: none; width: 20px; height: 20px; border: 2px solid var(--text-muted); border-radius: 5px; color: var(--transparent); transition: background-color .15s, border-color .15s, color .15s; }
  .confirm:has(input:focus-visible) .box { box-shadow: 0 0 0 3px var(--focus-halo-strong); }
  .confirm.checked { border-left-color: var(--ok); background: var(--ok-fill); color: var(--ok-text); }
  .confirm.checked .box { border-color: var(--ok-text); background: var(--ok-text); color: var(--ok-fill); }
  /* The override is a warning until it is ticked, so it reads apart from the plain confirmation below it. */
  .acks { display: flex; flex-direction: column; gap: .5rem; }
  .confirm.override:not(.checked) { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .confirm.override:not(.checked):hover { border-color: var(--warn); }
  .confirm.override:not(.checked) .box { border-color: var(--warn-text); }
  .confirm strong { color: inherit; }

  /* The result: admitting now, the PIN to read out, and the facts. */
  .hero { display: flex; align-items: center; gap: .85rem; margin: 0; padding: .9rem 1rem; border: 1px solid var(--ok-border); border-left: 5px solid var(--ok); border-radius: 12px; background: var(--ok-fill); color: var(--ok-text); }
  .live-dot { flex: none; width: 14px; height: 14px; margin: 0 .3rem; border-radius: 50%; background: var(--ok); animation: live-pulse 1.8s infinite; }
  @keyframes live-pulse { 0% { box-shadow: 0 0 0 0 var(--ok-border); } 70% { box-shadow: 0 0 0 10px var(--transparent); } 100% { box-shadow: 0 0 0 0 var(--transparent); } }
  .lbl { font-size: .7rem; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; }
  .hero h3 { color: var(--text-main); font-size: 1.05rem; overflow-wrap: anywhere; }
  .big-pin { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 1rem; padding: .9rem 1rem; border-radius: 12px; background: var(--header-strong); color: var(--on-dark); }
  .big-text { min-width: 0; }
  .big-pin .k { color: var(--accent-cyan-soft); font-size: .7rem; letter-spacing: .1em; }
  .big-pin code { display: block; font-size: 2rem; font-weight: 700; letter-spacing: .2em; overflow-wrap: anywhere; }
  .big-pin code.long { font-size: 1.3rem; letter-spacing: .08em; }
  .s { color: var(--side-text); font-size: .8rem; }
  .light { display: inline-flex; align-items: center; gap: .45rem; margin: 0; border-color: var(--cyan-chip-border); background: var(--chip-on-dark); color: var(--on-dark); }
  .light:hover:not(:disabled) { border-color: var(--accent-cyan-soft); background: var(--side-active); color: var(--on-dark); }
  .done-facts { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: .4rem 1rem; margin: 0; font-size: .88rem; }
  .done-facts dt { color: var(--text-muted); }
  .done-facts dd { margin: 0; font-weight: 600; overflow-wrap: anywhere; }

  /* The confirmation in a corner: it says the PIN was copied and fades by itself. */
  .toast { position: fixed; bottom: 1.5rem; left: 50%; display: none; align-items: center; gap: .5rem; transform: translateX(-50%); max-width: calc(100% - 2rem); padding: .5rem .9rem; border: 1px solid var(--action-border); border-radius: 10px; background: var(--hover-fill); color: var(--action-hover); box-shadow: var(--shadow-dialog); font-size: .88rem; font-weight: 600; z-index: 2; }
  .toast.show { display: flex; }

  /* Footer: it stays at the bottom of the dialog while the content scrolls. */
  .foot { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .foot button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; white-space: nowrap; }
  .cancel { background: var(--transparent); color: var(--text-muted); }
  .cancel:hover:not(:disabled) { background: var(--bg-secondary); border-color: var(--border-subtle); color: var(--text-main); }
  .close-btn { border-color: var(--action-border); background: var(--bg-raised); color: var(--action-hover); }

  @media (max-width: 520px) {
    .facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .fact:nth-child(2) { border-right: 0; }
    .fact:last-child { grid-column: 1 / -1; border-top: 1px solid var(--border-subtle); }
    .aud { grid-template-columns: minmax(0, 1fr); }
    .foot { flex-direction: column-reverse; }
    .foot button { width: 100%; }
  }
</style>

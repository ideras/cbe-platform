<script>
  import { onMount, tick, untrack } from 'svelte';
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  // Deleting one exam. It cannot be undone, so the dialog says exactly what goes and what stays, and unlocks its button only
  // after deliberate acts that fit what is lost. An exam nobody took asks for one tick. An exam students took also deletes their
  // submissions, events and screenshots, so it asks first for the tick that the proctor has downloaded them (with a way to do it
  // from here) and then for the exam's code, the first eight characters of its ID, typed by hand. The safe button has the first
  // focus, a click on the backdrop nudges the dialog instead of dismissing it, and while the server works nothing can be closed.
  // The dialog stays open after the deletion with what happened, so the result and the reminder about copies are never lost to an
  // automatic close.
  //
  // `exam` is the exam the dialog was opened for; `live` is the same exam as the dashboard last heard it, or null once it is gone.
  // Only the submission count follows `live`: the proctor confirms the number of submissions they are shown, so a change in it
  // takes the ticks back, and the server refuses a deletion whose number is not the exam's.
  let { exam, live = null, className = '', downloadNote = '', onDownload, onClose, onDeleted, onFailed } = $props();
  const code = $derived(exam.exam_id.slice(0, 8));
  const rest = $derived(exam.exam_id.slice(8));

  // The submissions the proctor is told about and confirms.
  let count = $state(untrack(() => exam.total_sessions ?? 0));
  const taken = $derived(count > 0);
  let downloaded = $state(false);
  let acknowledged = $state(false);
  let typed = $state('');
  // form, deleting or done.
  let stage = $state('form');
  let result = $state(null);
  let problem = $state('');
  let keep = $state(null);
  let codeInput = $state(null);

  const busy = $derived(stage === 'deleting');
  const done = $derived(stage === 'done');
  const matches = $derived(typed.toLowerCase() === code.toLowerCase());
  const unlocked = $derived(stage === 'form' && (taken ? downloaded && matches : acknowledged));
  const pending = $derived(done ? result.files_pending : 0);

  // The dialog is open once Modal has mounted, which is before this component's own mount.
  onMount(() => keep?.focus());

  const plural = (n, noun) => `${n.toLocaleString()} ${noun}${n === 1 ? '' : 's'}`;
  // If the number of submissions moves while the dialog is open, what was ticked was for another number: ask again.
  $effect(() => {
    const now = live?.total_sessions;
    if (stage !== 'form' || now === undefined || now === count) return;
    count = now;
    downloaded = acknowledged = false;
    typed = '';
  });
  // Unticking the download takes the typed code back, so the two acts always happen in order; ticking moves on to the field.
  async function downloadedChanged(event) {
    downloaded = event.currentTarget.checked;
    if (!downloaded) { typed = ''; return; }
    await tick();
    codeInput?.focus();
  }
  async function remove() {
    if (!unlocked) return;
    problem = '';
    stage = 'deleting';
    let value;
    try {
      value = await api.DeleteExam(exam.exam_id, count, true);
    } catch (err) {
      // The exam list is read again whatever went wrong: it shows whether the exam is still there and what it has now.
      problem = String(err);
      stage = 'form';
      onFailed?.();
      return;
    }
    result = value;
    stage = 'done';
    onDeleted?.(value);
  }
</script>
<Modal title="Delete exam" eyebrow="Cannot be undone" titleId="delete-exam-title" tone="deep alarm" icon="fileX" width="560px" {busy} {onClose}>
  <form onsubmit={event => { event.preventDefault(); remove(); }}>
    <div class="content">
      {#if done}
        <div class="hero" role="status">
          <span class="badge"><Icon name="check" size={20} /></span>
          <div>
            <div class="lbl">Exam deleted</div>
            <h3>{result.title}</h3>
            <p>{result.sessions > 0 ? `${plural(result.sessions, 'submission')} and ${plural(result.screenshots, 'screenshot')} removed` : `Removed from ${className}`}</p>
          </div>
        </div>
        {#if pending > 0}
          <p class="tip problem" role="alert"><Icon name="error" size={15} /><span>{plural(pending, 'screenshot file')} could not be removed from the server's disk yet. The exam and its records are gone; the server keeps trying and removes the files as soon as it can.</span></p>
        {/if}
        {#if result.sessions > 0}
          <p class="tip"><Icon name="warning" size={15} /><span>Your downloaded submissions, exports and backups still exist; they are the only copy now.</span></p>
        {/if}
      {:else}
        <div class="exam">
          <span class="ico"><Icon name="exams" size={17} /></span>
          <div class="who">
            <div class="t">{exam.title}</div>
            {#if className}<div class="c">{className}</div>{/if}
            <div class="id"><b>{code}</b>{rest}</div>
          </div>
          <span class="tag" class:clean={!taken} class:taken>{taken ? plural(count, 'submission') : 'Never taken'}</span>
        </div>

        <ul class="impact" aria-label="What deleting does">
          {#if taken}
            <li class="imp del">
              <span class="mark"><Icon name="close" size={13} /></span>
              <div>
                <div class="h">Deleted forever</div>
                <div class="s">The exam, every student's answers, events and screenshots.</div>
              </div>
              <div class="n">{count.toLocaleString()}<small>{count === 1 ? 'submission' : 'submissions'}</small></div>
            </li>
            <li class="imp del">
              <span class="mark"><Icon name="close" size={13} /></span>
              <div>
                <div class="h">PIN stops working</div>
                <div class="s">Nobody can log in to this exam again.</div>
              </div>
            </li>
            <li class="imp outside">
              <span class="mark"><Icon name="warning" size={13} /></span>
              <div>
                <div class="h">Not touched — your copies</div>
                <div class="s">Downloads, exports and backups made earlier.</div>
              </div>
            </li>
          {:else}
            <li class="imp keep">
              <span class="mark"><Icon name="check" size={13} /></span>
              <div>
                <div class="h">Nothing is lost</div>
                <div class="s">Nobody has taken it, so there are no answers or screenshots.</div>
              </div>
            </li>
            <li class="imp del">
              <span class="mark"><Icon name="close" size={13} /></span>
              <div>
                <div class="h">PIN stops working</div>
                <div class="s">The exam is removed and its PIN no longer works.</div>
              </div>
            </li>
          {/if}
          <li class="imp info">
            <span class="mark"><Icon name="info" size={13} /></span>
            <div>
              <div class="h">To give it again</div>
              <div class="s">Import the exam file again.</div>
            </div>
          </li>
        </ul>

        {#if taken}
          <div class="gate">
            <div class="gate-title">Before you continue</div>
            <div class="step" class:done={downloaded}>
              <span class="num">1</span>
              <div class="body">
                <label class="confirm" class:checked={downloaded} class:locked={stage !== 'form'}>
                  <input type="checkbox" checked={downloaded} disabled={stage !== 'form'} onchange={downloadedChanged} />
                  <span class="box"><Icon name="check" size={14} /></span>
                  <span>I have downloaded the submissions for this exam.</span>
                </label>
                <button type="button" class="link-btn" disabled={stage !== 'form'} onclick={() => onDownload?.(exam)}><Icon name="download" size={13} />Not yet? Download submissions…</button>
                {#if downloadNote}<p class="note"><Icon name="check" size={13} /><span>{downloadNote}</span></p>{/if}
              </div>
            </div>
            <div class="step" class:locked={!downloaded} class:done={matches}>
              <span class="num">2</span>
              <div class="body">
                <label class="type-label" for="delete-code">Type the exam code <code>{code}</code> to unlock</label>
                <div class="type-row">
                  <input id="delete-code" class="code-input" class:match={matches} bind:this={codeInput} bind:value={typed} type="text" autocomplete="off" spellcheck="false"
                    placeholder={code} maxlength={code.length} disabled={!downloaded || stage !== 'form'} onpaste={event => event.preventDefault()} />
                  <span class="state" class:on={matches} aria-hidden="true"><Icon name="check" size={13} /></span>
                </div>
              </div>
            </div>
          </div>
        {:else}
          <div class="gate">
            <label class="confirm" class:checked={acknowledged} class:locked={stage !== 'form'}>
              <input type="checkbox" bind:checked={acknowledged} disabled={stage !== 'form'} />
              <span class="box"><Icon name="check" size={14} /></span>
              <span>I want to delete “{exam.title}”.</span>
            </label>
          </div>
        {/if}

        {#if busy}
          <div class="running">
            <div class="row"><span>{taken ? 'Deleting the exam and its submissions…' : 'Deleting the exam…'}</span></div>
            <div class="track" role="progressbar" aria-label="Deleting the exam"><span></span></div>
          </div>
        {/if}
      {/if}

      {#if problem}<p class="tip problem" role="alert"><Icon name="error" size={15} /><span>{problem}</span></p>{/if}
    </div>

    <div class="foot">
      {#if done}
        <button type="button" class="primary" onclick={onClose}>Close</button>
      {:else}
        <button type="button" class="primary" bind:this={keep} disabled={busy} onclick={onClose}>Keep exam</button>
        <button type="submit" class="remove" disabled={!unlocked}>
          <Icon name="deleteExam" size={16} />
          <span>{busy ? 'Deleting…' : taken ? `Delete exam and ${plural(count, 'submission')}` : 'Delete exam'}</span>
        </button>
      {/if}
    </div>
  </form>
</Modal>
<style>
  p { margin: 0; overflow-wrap: anywhere; }
  h3 { margin: 0; }
  .content { display: flex; flex-direction: column; gap: 1rem; padding: 1.1rem; }

  /* The exam being deleted, and whether anyone took it. */
  .exam { display: flex; align-items: center; gap: .75rem; padding: .7rem .85rem; border: 1px solid var(--border-subtle); border-radius: 10px; background: var(--bg-primary); }
  .ico { display: grid; place-items: center; flex: none; width: 34px; height: 34px; border-radius: 9px; background: var(--selection-fill); color: var(--action-primary); }
  .who { min-width: 0; }
  .t { font-weight: 700; overflow-wrap: anywhere; }
  .c { color: var(--text-muted); font-size: .82rem; overflow-wrap: anywhere; }
  .id { margin-top: .1rem; color: var(--text-muted); font-family: var(--font-mono); font-size: .76rem; word-break: break-all; }
  .id b { padding: 0 .2rem; border-radius: 3px; background: var(--alert-fill); color: var(--alert-text); }
  .tag { margin-left: auto; padding: .12rem .55rem; border-radius: 999px; font-size: .74rem; font-weight: 700; white-space: nowrap; }
  .tag.clean { background: var(--ok-fill); color: var(--ok-text); }
  .tag.taken { background: var(--alert-fill); color: var(--alert-text); }

  /* What deleting does: what is destroyed, what stays, what it does not reach, and how to get the exam back. */
  .impact { display: flex; flex-direction: column; gap: .45rem; margin: 0; padding: 0; list-style: none; }
  .imp { display: flex; align-items: flex-start; gap: .7rem; padding: .6rem .8rem; border: 1px solid; border-radius: 10px; font-size: .88rem; }
  .mark { display: grid; place-items: center; flex: none; width: 24px; height: 24px; margin-top: .05rem; border-radius: 50%; }
  .h { font-weight: 700; }
  .s { font-size: .8rem; }
  .n { margin-left: auto; font-family: var(--font-mono); font-weight: 700; text-align: right; white-space: nowrap; }
  .n small { display: block; font-size: .75rem; font-weight: 500; }
  .imp.del { border-color: var(--alert-border); background: var(--alert-fill); color: var(--alert-text); }
  .imp.del .mark { background: var(--alert-strong); color: var(--paper); }
  .imp.keep { border-color: var(--ok-border); background: var(--ok-fill); color: var(--ok-text); }
  .imp.keep .mark { background: var(--ok-text); color: var(--ok-fill); }
  .imp.outside { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .imp.outside .mark { background: var(--warn-text); color: var(--warn-fill); }
  .imp.info { border-color: var(--action-border); background: var(--hover-fill); color: var(--action-hover); }
  .imp.info .mark { background: var(--action-hover); color: var(--hover-fill); }

  /* The gate: one tick for an exam nobody took; for one students took, the download and then the code. */
  .gate { display: flex; flex-direction: column; gap: .8rem; padding: .85rem; border: 1px dashed var(--alert-border); border-radius: 12px; background: var(--bg-primary); }
  .gate-title { color: var(--alert-text); font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .step { display: flex; align-items: flex-start; gap: .6rem; }
  .num { display: grid; place-items: center; flex: none; width: 22px; height: 22px; margin-top: .05rem; border-radius: 50%; background: var(--bg-secondary); color: var(--text-main); font-size: .72rem; font-weight: 700; transition: background-color .15s, color .15s; }
  .step.done .num { background: var(--alert-strong); color: var(--paper); }
  .step.locked { opacity: .55; }
  .body { display: flex; flex: 1; flex-direction: column; gap: .4rem; min-width: 0; }
  /* The real checkbox stays in the label, invisible, so the keyboard and screen readers keep a control; .box is what is drawn. */
  .confirm { position: relative; display: flex; align-items: flex-start; gap: .65rem; font-size: .9rem; font-weight: 600; cursor: pointer; user-select: none; }
  .confirm.locked { cursor: default; }
  .confirm input { position: absolute; width: 20px; height: 20px; margin: 0; opacity: 0; pointer-events: none; }
  .box { display: grid; place-items: center; flex: none; width: 20px; height: 20px; margin-top: .08rem; border: 2px solid var(--text-muted); border-radius: 5px; color: var(--transparent); transition: background-color .15s, border-color .15s, color .15s; }
  .confirm:has(input:focus-visible) .box { box-shadow: 0 0 0 3px var(--alert-halo); }
  .confirm.checked { color: var(--alert-text); }
  .confirm.checked .box { border-color: var(--alert-strong); background: var(--alert-strong); color: var(--paper); }
  .link-btn { display: inline-flex; align-items: center; gap: .35rem; width: fit-content; min-height: 0; margin: 0; padding: 0; border: 0; border-radius: 4px; background: var(--transparent); box-shadow: none; color: var(--action-hover); font-size: .8rem; font-weight: 600; }
  .link-btn:hover:not(:disabled) { background: var(--transparent); color: var(--action-primary); text-decoration: underline; }
  .link-btn:disabled { background: var(--transparent); color: var(--muted-dot); }
  .note { display: flex; align-items: flex-start; gap: .35rem; color: var(--ok-text); font-size: .8rem; }
  .note :global(svg) { margin-top: .15rem; }
  .type-label { display: block; margin-bottom: .35rem; color: var(--text-muted); font-size: .85rem; }
  .type-label code { padding: .05rem .3rem; border-radius: 4px; background: var(--alert-fill); color: var(--alert-text); font-weight: 700; }
  .type-row { display: flex; align-items: center; gap: .5rem; }
  .type-row .code-input { flex: 1; min-width: 0; height: 40px; padding: 0 .75rem; border-radius: 9px; background: var(--bg-raised); font-family: var(--font-mono); font-size: .95rem; font-weight: 700; letter-spacing: .05em; }
  .type-row .code-input:focus-visible { border-color: var(--alert); background: var(--bg-raised); box-shadow: 0 0 0 3px var(--alert-halo); }
  .type-row .code-input.match, .type-row .code-input.match:focus-visible { border-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }
  .type-row .code-input:disabled { background: var(--bg-secondary); }
  .code-input:disabled::placeholder { color: var(--muted-dot); }
  .state { display: grid; place-items: center; flex: none; width: 24px; height: 24px; border-radius: 50%; color: var(--transparent); }
  .state.on { background: var(--alert-strong); color: var(--paper); }

  /* Work in flight. The server reports no count, so the bar only shows that it is working. */
  .running { display: flex; flex-direction: column; gap: .4rem; }
  .row { color: var(--text-muted); font-size: .82rem; }
  .track { height: 8px; overflow: hidden; border-radius: 999px; background: var(--bg-secondary); }
  .track span { display: block; width: 40%; height: 100%; border-radius: inherit; background: var(--bar-alarm); animation: sweep 1.1s ease-in-out infinite alternate; }
  @keyframes sweep { from { transform: translateX(-100%); } to { transform: translateX(250%); } }

  /* The result: a quiet grey, since the exam is gone rather than something that succeeded. */
  .hero { display: flex; align-items: center; gap: .85rem; padding: .9rem 1rem; border: 1px solid var(--border-subtle); border-left: 5px solid var(--muted-dot); border-radius: 12px; background: var(--bg-secondary); color: var(--text-main); }
  .badge { display: grid; place-items: center; flex: none; width: 40px; height: 40px; border-radius: 50%; background: var(--text-muted); color: var(--bg-primary); }
  .lbl { font-size: .7rem; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; }
  .hero h3 { color: inherit; font-size: 1.05rem; overflow-wrap: anywhere; }
  .hero p { font-size: .85rem; }
  .tip { display: flex; align-items: flex-start; gap: .5rem; margin: 0; padding: .55rem .75rem; border: 1px solid var(--warn-border); border-left: 4px solid var(--warn); border-radius: 9px; background: var(--warn-fill); color: var(--warn-text); font-size: .85rem; font-weight: 400; }
  .tip :global(svg) { margin-top: .15rem; }
  .tip.problem { border-color: var(--alert-border); border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }

  /* Footer: it stays at the bottom of the dialog while the content scrolls. */
  .foot { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .foot button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; white-space: nowrap; }
  .remove { border-color: var(--alert-strong); background: var(--alert-strong); color: var(--paper); box-shadow: 0 3px 10px var(--alert-border); }
  .remove:hover:not(:disabled) { border-color: var(--alert-deep); background: var(--alert-deep); color: var(--paper); }
  .remove:disabled { border-color: var(--border-subtle); background: var(--bg-secondary); color: var(--muted-dot); box-shadow: none; }

  @media (max-width: 520px) {
    .exam { flex-wrap: wrap; }
    .tag { margin-left: 0; }
    .imp { flex-wrap: wrap; }
    .n { margin-left: 2.2rem; text-align: left; }
    .foot { flex-direction: column-reverse; }
    .foot button { width: 100%; }
  }
</style>

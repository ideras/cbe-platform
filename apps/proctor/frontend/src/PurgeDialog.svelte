<script>
  import { onMount, tick } from 'svelte';
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  // Purging the screenshots of one exam, after grading. It cannot be undone, so the dialog says exactly what goes, what stays
  // and what it does not reach, and unlocks its button only after two deliberate acts: ticking that grading is final and
  // typing the exam's code (the first eight characters of its ID). The safe button has the first focus, a click on the backdrop
  // nudges the dialog instead of dismissing it, and while the server works nothing can be closed. The dialog stays open after
  // the purge with what happened, so the result and the reminder about copies are never lost to an automatic close. The server
  // does not tell how many screenshots an exam has before a purge, so only the result counts them.
  let { exam, onClose, onPurged, onFailed } = $props();
  const code = $derived(exam.exam_id.slice(0, 8));
  const rest = $derived(exam.exam_id.slice(8));
  // How many failed image IDs the result names before saying how many more there are.
  const NAMED_FAILURES = 20;
  const impact = [
    { tone: 'del', icon: 'close', heading: 'Deleted forever', text: 'All stored screenshots for this exam.' },
    { tone: 'del', icon: 'ban', heading: 'Disabled permanently', text: 'No further images can be captured for this exam.' },
    { tone: 'keep', icon: 'check', heading: 'Kept', text: 'Answers and events (focus losses, heartbeats, locks).' },
    { tone: 'outside', icon: 'warning', heading: 'Not touched — delete separately', text: 'Downloads, exports and backups made earlier.' },
  ];

  let acknowledged = $state(false);
  let typed = $state('');
  // form, purging or done.
  let stage = $state('form');
  let result = $state(null);
  let problem = $state('');
  let keep = $state(null);
  let codeInput = $state(null);

  const busy = $derived(stage === 'purging');
  const done = $derived(stage === 'done');
  const matches = $derived(typed.toLowerCase() === code.toLowerCase());
  const unlocked = $derived(acknowledged && matches && stage === 'form');
  const partial = $derived(done && !result.complete);
  const failed = $derived(done ? result.failed_screenshot_ids : []);
  const deletedText = $derived(done ? (result.deleted === 0 ? 'No stored screenshots to delete' : `${plural(result.deleted, 'image')} deleted`) : '');

  // The dialog is open once Modal has mounted, which is before this component's own mount.
  onMount(() => keep?.focus());

  const plural = (n, noun) => `${n.toLocaleString()} ${noun}${n === 1 ? '' : 's'}`;
  // Unticking takes the typed code back, so the two acts always happen in order; ticking moves on to the field.
  async function acknowledge(event) {
    acknowledged = event.currentTarget.checked;
    if (!acknowledged) { typed = ''; return; }
    await tick();
    codeInput?.focus();
  }
  async function purge() {
    if (!unlocked) return;
    problem = '';
    stage = 'purging';
    let value;
    try {
      value = await api.PurgeScreenshots(exam.exam_id, true);
    } catch (err) {
      // The server may have done part of it before failing, so the list is read again; the gate stays as the proctor left it.
      problem = String(err);
      stage = 'form';
      onFailed?.();
      return;
    }
    result = value;
    stage = 'done';
    onPurged?.(value);
  }
</script>
<Modal title="Purge screenshots" eyebrow="Irreversible" titleId="purge-title" tone="deep alarm" icon="purge" width="560px" {busy} {onClose}>
  <form onsubmit={event => { event.preventDefault(); purge(); }}>
    <div class="content">
      {#if done}
        <div class="hero" class:partial role="status">
          <span class="badge"><Icon name={partial ? 'warning' : 'check'} size={22} /></span>
          <div>
            <h3>{partial ? 'Screenshots partly purged' : 'Screenshots purged'}</h3>
            <p>{deletedText}{partial ? `; ${plural(failed.length, 'image')} could not be deleted` : ''} · capture disabled for this exam</p>
          </div>
        </div>
        {#if partial}
          <p class="tip problem" role="alert"><Icon name="error" size={15} /><span>PARTIAL: retry for image IDs {failed.slice(0, NAMED_FAILURES).join(', ')}{failed.length > NAMED_FAILURES ? ` and ${failed.length - NAMED_FAILURES} more` : ''}. Purge this exam again to try them once more.</span></p>
        {/if}
        <p class="tip"><Icon name="warning" size={15} /><span>Remember: any downloads, exports or backups that contain these screenshots still exist and must be deleted by hand.</span></p>
      {:else}
        <div class="exam">
          <span class="ico"><Icon name="exams" size={17} /></span>
          <div>
            <div class="t">{exam.title}</div>
            <div class="id"><b>{code}</b>{rest}</div>
          </div>
        </div>

        <ul class="impact" aria-label="What a purge does">
          {#each impact as item (item.heading)}
            <li class="imp {item.tone}">
              <span class="mark"><Icon name={item.icon} size={13} /></span>
              <div>
                <div class="h">{item.heading}</div>
                <div class="s">{item.text}</div>
              </div>
            </li>
          {/each}
        </ul>

        <div class="gate">
          <div class="gate-title">Before you continue</div>
          <label class="confirm" class:checked={acknowledged} class:locked={stage !== 'form'}>
            <input type="checkbox" checked={acknowledged} disabled={stage !== 'form'} onchange={acknowledge} />
            <span class="box"><Icon name="check" size={14} /></span>
            <span>Grading is finalized and I confirm screenshot deletion for this exam.</span>
          </label>
          <div>
            <label class="type-label" for="purge-code">Type the exam code <code>{code}</code> to unlock</label>
            <div class="type-row">
              <input id="purge-code" class="code-input" class:match={matches} bind:this={codeInput} bind:value={typed} type="text" autocomplete="off" spellcheck="false"
                placeholder={code} maxlength={code.length} disabled={!acknowledged || stage !== 'form'} onpaste={event => event.preventDefault()} />
              <span class="state" class:on={matches} aria-hidden="true"><Icon name="check" size={13} /></span>
            </div>
          </div>
        </div>

        {#if busy}
          <div class="running">
            <div class="row"><span>Deleting screenshots…</span></div>
            <div class="track" role="progressbar" aria-label="Purging screenshots"><span></span></div>
          </div>
        {/if}
      {/if}

      {#if problem}<p class="tip problem" role="alert"><Icon name="error" size={15} /><span>{problem}</span></p>{/if}
    </div>

    <div class="foot">
      {#if done}
        <button type="button" class="primary" onclick={onClose}>Close</button>
      {:else}
        <button type="button" class="primary" bind:this={keep} disabled={busy} onclick={onClose}>Keep screenshots</button>
        <button type="submit" class="purge" disabled={!unlocked}>
          <Icon name="deleteExam" size={16} />
          <span>{busy ? 'Purging…' : 'Purge screenshots'}</span>
        </button>
      {/if}
    </div>
  </form>
</Modal>
<style>
  p { margin: 0; overflow-wrap: anywhere; }
  h3 { margin: 0; }
  .content { display: flex; flex-direction: column; gap: 1rem; padding: 1.1rem; }

  /* The exam being purged. */
  .exam { display: flex; align-items: center; gap: .75rem; padding: .7rem .85rem; border: 1px solid var(--border-subtle); border-radius: 10px; background: var(--bg-primary); }
  .ico { display: grid; place-items: center; flex: none; width: 34px; height: 34px; border-radius: 9px; background: var(--selection-fill); color: var(--action-primary); }
  .t { font-weight: 700; overflow-wrap: anywhere; }
  .id { color: var(--text-muted); font-family: var(--font-mono); font-size: .78rem; word-break: break-all; }
  .id b { padding: 0 .2rem; border-radius: 3px; background: var(--alert-fill); color: var(--alert-text); }

  /* What the purge does, as four rows: two that destroy, one that stays, one it does not reach. */
  .impact { display: flex; flex-direction: column; gap: .45rem; margin: 0; padding: 0; list-style: none; }
  .imp { display: flex; align-items: flex-start; gap: .7rem; padding: .6rem .8rem; border: 1px solid; border-radius: 10px; font-size: .88rem; }
  .mark { display: grid; place-items: center; flex: none; width: 24px; height: 24px; margin-top: .05rem; border-radius: 50%; }
  .h { font-weight: 700; }
  .s { font-size: .8rem; }
  .imp.del { border-color: var(--alert-border); background: var(--alert-fill); color: var(--alert-text); }
  .imp.del .mark { background: var(--alert-strong); color: var(--paper); }
  .imp.keep { border-color: var(--ok-border); background: var(--ok-fill); color: var(--ok-text); }
  .imp.keep .mark { background: var(--ok-text); color: var(--ok-fill); }
  .imp.outside { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .imp.outside .mark { background: var(--warn-text); color: var(--warn-fill); }

  /* The gate: tick, then type the code. */
  .gate { display: flex; flex-direction: column; gap: .75rem; padding: .85rem; border: 1px dashed var(--alert-border); border-radius: 12px; background: var(--bg-primary); }
  .gate-title { color: var(--alert-text); font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  /* The real checkbox stays in the label, invisible, so the keyboard and screen readers keep a control; .box is what is drawn. */
  .confirm { position: relative; display: flex; align-items: flex-start; gap: .65rem; font-size: .9rem; font-weight: 600; cursor: pointer; user-select: none; }
  .confirm.locked { cursor: default; }
  .confirm input { position: absolute; width: 20px; height: 20px; margin: 0; opacity: 0; pointer-events: none; }
  .box { display: grid; place-items: center; flex: none; width: 20px; height: 20px; margin-top: .08rem; border: 2px solid var(--text-muted); border-radius: 5px; color: var(--transparent); transition: background-color .15s, border-color .15s, color .15s; }
  .confirm:has(input:focus-visible) .box { box-shadow: 0 0 0 3px var(--alert-halo); }
  .confirm.checked { color: var(--alert-text); }
  .confirm.checked .box { border-color: var(--alert-strong); background: var(--alert-strong); color: var(--paper); }
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

  /* The result. */
  .hero { display: flex; align-items: center; gap: .85rem; padding: .9rem 1rem; border: 1px solid var(--ok-border); border-left: 5px solid var(--ok); border-radius: 12px; background: var(--ok-fill); color: var(--ok-text); }
  .hero.partial { border-color: var(--warn-border); border-left-color: var(--warn); background: var(--warn-fill); color: var(--warn-text); }
  .badge { display: grid; place-items: center; flex: none; width: 40px; height: 40px; border-radius: 50%; background: var(--ok-text); color: var(--ok-fill); }
  .hero.partial .badge { background: var(--warn-text); color: var(--warn-fill); }
  .hero h3 { color: inherit; font-size: 1.05rem; }
  .hero p { font-size: .85rem; }
  .tip { display: flex; align-items: flex-start; gap: .5rem; margin: 0; padding: .55rem .75rem; border: 1px solid var(--warn-border); border-left: 4px solid var(--warn); border-radius: 9px; background: var(--warn-fill); color: var(--warn-text); font-size: .85rem; font-weight: 400; }
  .tip :global(svg) { margin-top: .15rem; }
  .tip.problem { border-color: var(--alert-border); border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }

  /* Footer: it stays at the bottom of the dialog while the content scrolls. */
  .foot { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .foot button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; white-space: nowrap; }
  .purge { border-color: var(--alert-strong); background: var(--alert-strong); color: var(--paper); box-shadow: 0 3px 10px var(--alert-border); }
  .purge:hover:not(:disabled) { border-color: var(--alert-deep); background: var(--alert-deep); color: var(--paper); }
  .purge:disabled { border-color: var(--border-subtle); background: var(--bg-secondary); color: var(--muted-dot); box-shadow: none; }

  @media (max-width: 520px) {
    .foot { flex-direction: column-reverse; }
    .foot button { width: 100%; }
  }
</style>

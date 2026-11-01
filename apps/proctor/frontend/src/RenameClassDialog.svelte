<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { message } from './errors.js';
  import { CLASS_NAME_MAX, checkClassName } from './class-name.js';
  // Renaming changes only the name: the class keeps its roster, its students and its exams, which the dialog lists so that is
  // plain. The field is judged the way the server will judge it (length, characters, another class's name), and a refusal the
  // server still gives stays under the field until the name is edited. `examCount` is the one the page shows and `others` the
  // names of the other classes.
  let { entry, examCount = entry.exam_count, others = [], onClose, onRenamed } = $props();
  // svelte-ignore state_referenced_locally
  let name = $state(entry.name);
  let input;
  let busy = $state(false);
  let problem = $state('');
  const check = $derived(checkClassName(name, entry.name, others));
  const tone = $derived(check.error ? 'err' : check.ready ? 'ok' : '');
  // The dialog is open by now (its Modal mounted first): the old name is selected, so typing replaces it.
  onMount(() => { input.focus(); input.select(); });
  async function save() {
    if (!check.ready || busy) return;
    busy = true; problem = '';
    try { onRenamed(await api.RenameClass(entry.class_id, check.name)); }
    catch (err) { problem = message(err); }
    finally { busy = false; }
  }
  function clear() { name = ''; problem = ''; input.focus(); }
</script>
<Modal title="Rename class" titleId="rename-class-title" tone="deep" icon="pencil" width="480px" subtitle={entry.name} {busy} {onClose}>
  <form onsubmit={event => { event.preventDefault(); save(); }}>
    <div class="content">
      <div class="field">
        <div class="field-top">
          <label for="rename-class-name">Class name</label>
          <span class="counter" class:near={check.length > CLASS_NAME_MAX - 10 && check.length <= CLASS_NAME_MAX} class:over={check.length > CLASS_NAME_MAX}>{check.length} / {CLASS_NAME_MAX}</span>
        </div>
        <div class="input-wrap" class:err={!!check.error || !!problem} class:busy>
          <input id="rename-class-name" bind:this={input} bind:value={name} autocomplete="off" spellcheck="false" placeholder="e.g. Compiladores I Q12027"
            disabled={busy} aria-invalid={!!check.error || !!problem} aria-describedby="rename-class-msg" oninput={() => problem = ''} />
          {#if name}<button type="button" class="clear" aria-label="Clear" disabled={busy} onclick={clear}><Icon name="close" size={15} /></button>{/if}
        </div>
        {#if problem}
          <p id="rename-class-msg" class="refused" role="alert">{problem}</p>
        {:else}
          <p id="rename-class-msg" class="msg {tone}" aria-live="polite">{check.message}</p>
        {/if}
      </div>
      <div>
        <p class="keeps-title" id="rename-keeps">Stays exactly as it is</p>
        <ul class="keeps" aria-labelledby="rename-keeps">
          <li class="keep"><Icon name="classes" size={13} /><b>{entry.student_count}</b> {entry.student_count === 1 ? 'student' : 'students'}</li>
          <li class="keep"><Icon name="exams" size={13} /><b>{examCount}</b> {examCount === 1 ? 'exam' : 'exams'}</li>
        </ul>
      </div>
    </div>
    <div class="actions">
      <span class="kbd-hint"><kbd>Enter</kbd> rename · <kbd>Esc</kbd> cancel</span>
      <button type="button" class="cancel" disabled={busy} onclick={onClose}>Cancel</button>
      <button type="submit" class="primary" disabled={!check.ready || busy}><Icon name="check" size={16} />{busy ? 'Renaming…' : 'Rename'}</button>
    </div>
  </form>
</Modal>
<style>
  .content { display: flex; flex-direction: column; gap: 1rem; padding: 1.1rem; }
  p { margin: 0; }
  .field-top { display: flex; align-items: baseline; justify-content: space-between; gap: .5rem; margin-bottom: .35rem; }
  label { font-size: .88rem; font-weight: 600; }
  .counter { color: var(--text-muted); font-family: var(--font-mono); font-size: .75rem; }
  .counter.near { color: var(--warn-text); }
  .counter.over { color: var(--alert-text); font-weight: 700; }
  .input-wrap { display: flex; align-items: center; height: 44px; padding: 0 .35rem 0 .75rem; border: 1px solid var(--control-border); border-radius: 9px; background: var(--bg-primary); transition: border-color .2s ease, box-shadow .2s ease, background-color .2s ease; }
  .input-wrap:focus-within { border-color: var(--action-primary); background: var(--paper); box-shadow: 0 0 0 3px var(--focus-halo); }
  .input-wrap.err { border-color: var(--alert); box-shadow: 0 0 0 3px var(--alert-halo); }
  .input-wrap.busy { background: var(--bg-secondary); border-color: var(--border-subtle); box-shadow: none; }
  .input-wrap input { flex: 1; min-width: 0; min-height: 0; padding: 0; border: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); font-size: 1rem; font-weight: 600; }
  .input-wrap input:focus-visible { outline: none; background: var(--transparent); box-shadow: none; }
  .input-wrap input::placeholder { font-weight: 400; }
  .clear { display: grid; place-items: center; flex: none; width: 30px; height: 30px; min-height: 0; margin: 0; padding: 0; border: 0; border-radius: 7px; background: var(--transparent); color: var(--text-muted); box-shadow: none; }
  .clear:hover:not(:disabled) { background: var(--bg-secondary); border-color: var(--transparent); color: var(--text-main); }
  .msg { min-height: 1.15em; margin-top: .35rem; color: var(--text-muted); font-size: .8rem; }
  .msg.err { color: var(--alert-text); }
  .msg.ok { color: var(--ok-text); }
  .refused { margin: .5rem 0 0; font-size: .88rem; overflow-wrap: anywhere; }
  .keeps-title { margin-bottom: .4rem; color: var(--text-muted); font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .keeps { display: flex; flex-wrap: wrap; gap: .4rem; margin: 0; padding: 0; list-style: none; }
  .keep { display: inline-flex; align-items: center; gap: .35rem; padding: .2rem .6rem; border: 1px solid var(--action-border); border-radius: 999px; background: var(--hover-fill); color: var(--action-hover); font-size: .82rem; }
  .keep :global(svg) { color: var(--action-primary); }
  .keep b { font-family: var(--font-mono); }
  .actions { display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; background: var(--bg-primary); border-top: 1px solid var(--border-subtle); }
  .kbd-hint { margin-right: auto; color: var(--text-muted); font-size: .75rem; }
  kbd { padding: .05rem .35rem; border: 1px solid var(--border-subtle); border-bottom-width: 2px; border-radius: 4px; background: var(--bg-raised); color: var(--text-muted); font-family: var(--font-mono); font-size: .72rem; }
  .actions button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; }
  .cancel { background: var(--transparent); color: var(--text-muted); }
  .cancel:hover:not(:disabled) { background: var(--bg-secondary); color: var(--text-main); }
  @media (max-width: 480px) {
    .kbd-hint { display: none; }
    .actions { flex-direction: column-reverse; }
    .actions button { width: 100%; }
  }
</style>

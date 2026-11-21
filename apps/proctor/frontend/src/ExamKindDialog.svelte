<script>
  import { tick, untrack } from 'svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import Modal from './Modal.svelte';

  let { file, detected, locked, working, onCancel, onValidate } = $props();
  // The parent opens a fresh dialog for each native selection. The radio choice belongs to that dialog only.
  let kind = $state(untrack(() => detected));
  let validateButton = $state();
  let focused = false;
  const id = $props.id();
  const kinds = {
    workspace: {
      name: 'Workspace Exam', icon: 'terminal',
      description: 'Students code in their own container, with files, an editor and a terminal.',
      features: ['Uses a course image or its own build', 'TODO list and optional hidden tests'],
    },
    question: {
      name: 'Traditional Exam', icon: 'exams',
      description: 'Students work through exercises with answer fields.',
      features: ['No container needed', 'Optional calculator'],
    },
  };
  // The picker can still hold the lock during the first render. Focus the action when it first becomes available.
  $effect(() => {
    const button = validateButton;
    if (!focused && !locked && button) {
      tick().then(() => {
        if (!button.isConnected || button.disabled) return;
        focused = true;
        button.focus();
      });
    }
  });
</script>

<Modal title="Confirm the exam kind" titleId="{id}-title" eyebrow="Import exam · step 1 of 2" icon="download"
  tone="deep" width="560px" closeLabel="Cancel import" onClose={onCancel}>
  <div class="content">
    <div class="file">
      <span class="file-icon"><Icon name="exams" size={17} /></span>
      <div class="file-detail">
        <div class="file-key">Selected file</div>
        <div class="file-name">{file}</div>
      </div>
      <span class="detected"><Icon name="check" size={12} />Detected: {kinds[detected].name}</span>
    </div>

    <fieldset disabled={locked}>
      <legend>Exam kind</legend>
      <div class="kinds">
        {#each Object.entries(kinds) as [value, item] (value)}
          <label class="kind" class:selected={kind === value}>
            <span class="card-head">
              <span class="kind-icon"><Icon name={item.icon} size={16} /></span>
              <span id="{id}-{value}-name">{item.name}</span>
              <input type="radio" name="{id}-kind" {value} bind:group={kind}
                aria-labelledby="{id}-{value}-name" aria-describedby="{id}-{value}-description" />
            </span>
            {#if detected === value}<span class="tag" aria-hidden="true">Detected</span>{/if}
            <span class="description" id="{id}-{value}-description">{item.description}</span>
            <span class="features">
              {#each item.features as feature (feature)}
                <span><Icon name="check" size={10} />{feature}</span>
              {/each}
            </span>
          </label>
        {/each}
      </div>
    </fieldset>

    <div class="override-slot" aria-live="polite" aria-atomic="true">
      {#if kind !== detected}
        <p class="override"><Icon name="warning" size={16} />
          <span>This differs from the detected kind. Validation will check whether the selected file is a <strong>{kinds[kind].name}</strong>.</span>
        </p>
      {/if}
    </div>

    <div class="progress">
      <span>Import:</span>
      <ol>
        <li aria-current="step">1 · Kind</li>
        <li><span aria-hidden="true">→</span> 2 · Validate file</li>
        <li><span aria-hidden="true">→</span> Exam added</li>
      </ol>
    </div>
  </div>
  <!-- Keep the footer in normal flow: native WebKit can stretch a 1fr dialog body to its full height limit. -->
  <footer class="actions">
    <button type="button" onclick={onCancel}>Cancel</button>
    <button type="button" class="primary" disabled={locked} bind:this={validateButton} onclick={() => onValidate(kind)}>
      {working ? 'Validating…' : `Validate as ${kinds[kind].name}`}
      <Icon name="arrowRight" size={16} />
    </button>
  </footer>
</Modal>

<style>
  .content { display: grid; gap: 1rem; padding: 1.1rem; }
  .file { display: flex; align-items: center; gap: .75rem; min-width: 0; padding: .65rem .85rem; border: 1px solid var(--border-subtle); border-radius: 10px; background: var(--bg-primary); }
  .file-icon, .kind-icon { display: grid; place-items: center; flex: none; }
  .file-icon { width: 34px; height: 34px; border-radius: 9px; background: var(--selection-fill); color: var(--action-primary); }
  .file-detail { flex: 1; min-width: 0; }
  .file-key, legend { font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .file-key { color: var(--text-muted); }
  .file-name { overflow-wrap: anywhere; font-family: var(--mono); font-weight: 700; }
  .detected, .tag { display: inline-flex; align-items: center; gap: .35rem; border-radius: 999px; background: var(--ok-fill); color: var(--ok-text); font-weight: 700; }
  .detected { flex: none; max-width: 100%; padding: .15rem .6rem; border: 1px solid var(--ok-border); font-size: .76rem; }
  .tag { width: fit-content; padding: .05rem .5rem; font-size: .7rem; }

  fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
  legend { margin-bottom: .45rem; padding: 0; color: var(--action-primary); }
  .kinds { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .6rem; }
  .kind { position: relative; display: flex; flex-direction: column; gap: .45rem; padding: .8rem .85rem; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--bg-primary); cursor: pointer; }
  .kind input { width: 18px; height: 18px; margin: 0 0 0 auto; cursor: inherit; }
  .kind:hover { border-color: var(--accent-cyan-soft); background: var(--hover-fill); }
  .kind.selected { border-color: var(--action-primary); background: var(--selection-fill); box-shadow: 0 3px 12px var(--action-border); }
  .kind:has(input:focus-visible) { outline: 2px solid var(--action-primary); outline-offset: 2px; }
  .kind input:focus-visible { outline: none; box-shadow: none; }
  fieldset:disabled .kind { cursor: default; }
  .card-head { display: flex; align-items: center; gap: .55rem; line-height: 1.2; font-weight: 700; }
  .kind-icon { width: 32px; height: 32px; border-radius: 8px; background: var(--bg-secondary); color: var(--text-muted); }
  .selected .kind-icon { background: var(--action-primary); color: var(--bg-primary); }
  .description { font-size: .82rem; color: var(--text-muted); }
  .features { display: grid; gap: .2rem; font-size: .78rem; color: var(--text-muted); }
  .features > span { display: flex; align-items: flex-start; gap: .4rem; }
  .features :global(svg) { flex: none; margin-top: .25rem; color: var(--action-primary); }

  .override-slot:not(:has(.override)) { position: absolute; }
  .override { display: flex; align-items: flex-start; gap: .55rem; margin: 0; padding: .55rem .75rem; border: 1px solid var(--warn-border); border-left: 4px solid var(--warn); border-radius: 9px; background: var(--warn-fill); color: var(--warn-text); font-size: .85rem; }
  .override :global(svg) { flex: none; margin-top: .15rem; }
  .progress, .progress ol, .progress li { display: flex; align-items: center; flex-wrap: wrap; gap: .35rem; }
  .progress { font-size: .82rem; color: var(--text-muted); }
  .progress ol { margin: 0; padding: 0; list-style: none; }
  .progress li { position: relative; padding: .08rem .5rem; border-radius: 999px; background: var(--bg-secondary); font-weight: 600; }
  .progress li + li { margin-left: 1rem; }
  .progress li span { position: absolute; left: -1.05rem; }
  .progress li[aria-current] { background: var(--action-primary); color: var(--bg-primary); }

  .actions { position: sticky; bottom: 0; z-index: 1; display: flex; justify-content: flex-end; gap: .6rem; padding: .8rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .actions button { display: inline-flex; justify-content: center; align-items: center; gap: .45rem; min-height: 40px; margin: 0; padding: .34rem 1.1rem; border-radius: 9px; font-weight: 700; }
  .actions button:not(.primary) { background: var(--transparent); color: var(--text-muted); }
  .actions button:not(.primary):hover { background: var(--bg-secondary); color: var(--text-main); }
  .actions :global(svg) { flex: none; }
  @media (max-width: 520px) {
    .kinds { grid-template-columns: 1fr; }
    .file { display: grid; grid-template-columns: auto minmax(0, 1fr); }
    .detected { grid-column: 2; justify-self: start; }
    .actions { flex-direction: column-reverse; }
  }
</style>

<script>
  import { onMount } from 'svelte';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  // Asks before the dashboard closes, from its Close button or the window's own. Closing the dashboard changes nothing on
  // the server: the exams go on without it. What it does take away is the one looking at the students, so the ones who are
  // locked wait until a proctor opens a dashboard again. locked is how many there are now, as the dashboard last heard it.
  let { locked = 0, onConfirm, onCancel } = $props();
  let keep = $state(null);
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }

  // The dialog is open once Modal has mounted, which is before this component's own mount. The focus goes to the choice that
  // changes nothing, so an Enter meant for something else cannot quit the application.
  onMount(() => keep?.focus());
</script>
<Modal title="Close the dashboard?" titleId="close-dialog-title" tone="deep" icon="power" width="480px" closeLabel="Keep it open" onClose={onCancel}>
  <div class="content">
    <p>All of its windows close and this computer stops monitoring.</p>
    {#if locked > 0}
      <p class="notice locked"><Icon name="warning" size={16} /><span><strong>{plural(locked, 'student')} {locked === 1 ? 'is' : 'are'} locked.</strong> They stay locked until a proctor opens a dashboard and unlocks them.</span></p>
    {/if}
    <p class="notice safe"><Icon name="shieldCheck" size={16} /><span><strong>{locked > 0 ? 'Students who are not locked keep working.' : 'Students keep working.'}</strong> Their exams, answers and timers live on the server, and you can open the dashboard again at any time.</span></p>
  </div>
  <div class="foot">
    <button type="button" class="danger" onclick={onConfirm}>Close the dashboard</button>
    <button type="button" class="primary" bind:this={keep} onclick={onCancel}>Keep it open</button>
  </div>
</Modal>
<style>
  p { margin: 0; overflow-wrap: anywhere; }
  .content { display: flex; flex-direction: column; gap: .8rem; padding: 1.1rem; }

  /* What the dashboard leaves alone, and what it cannot look after once it is closed. */
  .notice { display: flex; align-items: flex-start; gap: .6rem; padding: .65rem .8rem; border: 1px solid var(--ok-border); border-left: 4px solid var(--ok); border-radius: 9px; background: var(--ok-fill); color: var(--ok-text); font-size: .9rem; }
  .notice :global(svg) { margin-top: .1rem; }
  .notice strong { color: inherit; }
  .notice.locked { border-color: var(--alert-border); border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }

  /* Footer: it stays at the bottom of the dialog while the content scrolls. */
  .foot { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .foot button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; white-space: nowrap; }

  @media (max-width: 480px) {
    .foot { flex-direction: column-reverse; }
    .foot button { width: 100%; }
  }
</style>

<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Monitoring from './Monitoring.svelte';
  // The monitoring window: the live roster of the exam the main window monitors. It reads the same dashboard state.
  let view = $state({ observation: 0, connected: false, loaded: false, busy: false, stale: true });
  let bridgeError = $state('');
  // False until the native side has answered once. `view` above is only a placeholder until then, so what it says (not connected,
  // no exam) is not yet true of anything, so the window says it is loading rather than warning of something that goes away a moment later.
  let ready = $state(false);
  let alive = true;
  const monitored = $derived(view.catalog?.exams?.find(entry => entry.exam_id === view.exam_id));
  // The title says how many students are locked, so the proctor notices it in another window.
  const lockedCount = $derived((view.roster?.sessions ?? []).filter(row => row.locked).length);
  $effect(() => { document.title = `${lockedCount ? `(${lockedCount} locked) ` : ''}${monitored ? `Monitoring — ${monitored.title} (${monitored.class_name})` : 'Monitoring'}`; });
  function apply(value) { if (alive && value.observation >= view.observation) { view = value; ready = true; } }
  onMount(() => {
    let polling = false;
    async function tick() {
      if (polling) return; polling = true;
      try { const value = await api.Snapshot(); if (alive) { bridgeError = ''; apply(value); } }
      catch { if (alive) bridgeError = 'Native dashboard connection unavailable. Displayed data is stale.'; }
      finally { polling = false; }
    }
    tick(); const timer = setInterval(tick, 500);
    return () => { alive = false; clearInterval(timer); };
  });
  async function refreshNow() { await api.Refresh(); apply(await api.Snapshot()); }
</script>
<main class="window" aria-busy={!ready}>
  {#if !ready}
    <!-- Nothing to report yet, so say that it is loading, for as long as it is. A native side that cannot answer at all is said instead. -->
    {#if bridgeError}<p role="alert">{bridgeError}</p>
    {:else}<p class="callout pending" role="status">Loading the dashboard…</p>{/if}
  {:else if !view.connected}<p class="callout warning">Not connected to the server. Connect from the main window; this window follows it.</p>
  {:else if !view.exam_id}<p class="callout warning">No exam is being monitored. Choose one with <strong>Monitor</strong> on the Exams tab of its class, or in the "Now admitting" bar of the main window.</p>
  {:else}<Monitoring {view} {bridgeError} onRefresh={refreshNow} />{/if}
</main>
<style>
  .window { max-width: calc(1240px + 2.5rem); min-width: 0; margin: 0 auto; padding: 1.25rem; }
  @media (max-width: 520px) { .window { padding: .75rem; } }
</style>

<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Monitoring from './Monitoring.svelte';
  // The monitoring window: the live roster of the exam the main window monitors. It reads the same dashboard state.
  let view = $state({ observation: 0, connected: false, loaded: false, busy: false, stale: true });
  let bridgeError = $state('');
  let alive = true;
  const monitored = $derived(view.catalog?.exams?.find(entry => entry.exam_id === view.exam_id));
  // The title says how many students are locked, so the proctor notices it in another window.
  const lockedCount = $derived((view.roster?.sessions ?? []).filter(row => row.locked).length);
  $effect(() => { document.title = `${lockedCount ? `(${lockedCount} locked) ` : ''}${monitored ? `Monitoring — ${monitored.title} (${monitored.class_name})` : 'Monitoring'}`; });
  function apply(value) { if (alive && value.observation >= view.observation) view = value; }
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
<main class="window">
  {#if !view.connected}<p class="callout warning">Not connected to the server. Connect from the main window; this window follows it.</p>
  {:else if !view.exam_id}<p class="callout warning">No exam is being monitored. Choose one with <strong>Monitor</strong> on the Exams tab of its class, or in the "Now admitting" bar of the main window.</p>
  {:else}<Monitoring {view} {bridgeError} onRefresh={refreshNow} />{/if}
</main>
<style>
  .window { padding: var(--space-4); }
</style>

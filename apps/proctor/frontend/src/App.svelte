<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Sidebar from './Sidebar.svelte';
  import Classes from './Classes.svelte';
  import Settings from './Settings.svelte';
  import AdmittingBar from './AdmittingBar.svelte';
  import ConnectionDialog from './ConnectionDialog.svelte';
  import CloseDialog from './CloseDialog.svelte';
  import Icon from './Icon.svelte';

  // The shell: it polls the native side for the dashboard's state and decides what is on screen. Classes (each with its exams) and
  // the "Now admitting" bar exist only while connected; everything else about a connection (address, token file, trying again)
  // lives in the Server connection dialog.
  let view = $state({ observation: 0, connected: false, connecting: false, loaded: false, busy: false, stale: true });
  let bridgeError = $state('');
  let section = $state('classes');
  let connectionOpen = $state(false);
  let closeAsked = $state(false);
  let home = $state(0);
  // Asks the classes page to open a class on its exams; n changes with every request, so asking again works.
  let openRequest = $state(null);
  let alive = true;

  const connection = $derived(view.connected ? (view.unreachable ? 'lost' : 'connected') : view.connecting ? 'connecting' : view.connect_error ? 'failed' : 'idle');
  const host = $derived(hostOf(view.server));
  function hostOf(address) { try { return new URL(address).host; } catch { return address ?? ''; } }

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
    // The window's own close button asks here too.
    const stopListening = api.OnCloseRequested(() => { closeAsked = true; });
    return () => { alive = false; clearInterval(timer); stopListening(); };
  });
  // A connection that has just been established (by hand or at startup) opens on the classes, as the dashboard always does.
  let wasConnected = false;
  $effect(() => { if (view.connected && !wasConnected) section = 'classes'; wasConnected = view.connected; });

  async function reload() { apply(await api.Snapshot()); }
  async function refreshNow() { await api.Refresh(); await reload(); }
  function showClass(classId) { section = 'classes'; openRequest = { classId, n: (openRequest?.n ?? 0) + 1 }; }
  function connected(value) { apply(value); connectionOpen = false; section = 'classes'; }
  function disconnected(value) { apply(value); connectionOpen = false; }
</script>
<div class="shell">
  <div class="rail">
    <Sidebar {section} {connection} {host} onSelect={id => { if (id === 'classes' && section === 'classes') home++; section = id; }} onConnection={() => connectionOpen = true} onQuit={() => closeAsked = true} />
  </div>
  <main>
    {#if bridgeError && !view.exam_id}<p role="alert">{bridgeError}</p>{/if}
    {#if !view.connected}
      <section class="welcome" aria-label="Connection">
        <Icon name="server" size={52} />
        {#if view.connecting}
          <h1>Connecting…</h1>
          <p class="muted">Reaching {host}. Classes and exams appear as soon as the server answers.</p>
        {:else}
          <h1>{view.connect_error ? 'Not connected' : 'Welcome'}</h1>
          {#if view.connect_error}<p role="alert">{view.connect_error}</p>
          {:else}<p class="muted">Connect to your exam server to manage classes and exams.</p>{/if}
          {#if view.notice}<p class="callout warning">{view.notice}</p>{/if}
          <button type="button" class="primary" onclick={() => connectionOpen = true}><Icon name="server" size={20} />Server connection…</button>
        {/if}
      </section>
    {:else}
      <AdmittingBar {view} onSelected={refreshNow} onOpenClass={showClass} />
      <div class="page" hidden={section !== 'classes'}>
        <Classes {view} {home} {openRequest} disabled={view.busy} onRefresh={refreshNow} onSelected={refreshNow} />
      </div>
      <!-- The Settings page reads the server's values each time it opens: another proctor may have saved since. -->
      {#if section === 'settings'}<div class="page"><Settings disabled={view.busy} /></div>{/if}
    {/if}
  </main>
</div>
{#if closeAsked}<CloseDialog onCancel={() => closeAsked = false} onConfirm={() => api.Quit()} />{/if}
{#if connectionOpen}<ConnectionDialog {view} onClose={() => connectionOpen = false} onConnected={connected} onChanged={reload} onDisconnected={disconnected} />{/if}

<style>
  .shell { display: grid; grid-template-columns: 17.5rem minmax(0, 1fr); min-height: 100vh; }
  .rail { position: sticky; top: 0; height: 100vh; }
  main { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); align-content: start; min-width: 0; padding: clamp(1.25rem, 3vw, 2.6rem) clamp(1rem, 3.2vw, 3rem) 3rem; }
  .page { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); align-content: start; min-width: 0; }
  .page[hidden] { display: none; }
  .welcome { display: grid; justify-items: center; gap: var(--space-2); max-width: 36rem; margin: 12vh auto 0; padding: var(--space-6) var(--space-5); text-align: center; color: var(--brand); background: var(--color-surface); border: 1px solid var(--panel-border); border-radius: var(--radius-card); box-shadow: var(--panel-shadow); }
  .welcome h1 { margin: var(--space-2) 0 0; font-size: 2rem; font-weight: 800; }
  .welcome p { margin: 0; color: var(--color-text-muted); }
  .welcome [role="alert"] { color: var(--color-danger); text-align: left; }
  .welcome button { display: inline-flex; align-items: center; gap: .5rem; margin-top: var(--space-3); }
  @media (max-width: 820px) {
    .shell { grid-template-columns: minmax(0, 1fr); }
    .rail { position: static; height: auto; }
  }
</style>

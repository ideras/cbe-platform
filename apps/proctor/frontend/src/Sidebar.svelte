<script>
  import Icon from './Icon.svelte';
  // The navigation rail. Server connection and Close always work; Classes needs a connection and is really disabled (not just
  // dimmed) while there is none. Exams are reached through the class they belong to. `connection` is idle | connecting |
  // connected | lost | failed.
  let { section, connection, host = '', onSelect, onConnection, onQuit } = $props();
  const connected = $derived(connection === 'connected' || connection === 'lost');
  const status = $derived({
    idle: ['Not connected', 'Open Server connection to begin.'],
    connecting: ['Connecting…', host],
    connected: ['Connected', host],
    lost: ['Server not answering', `${host} · retrying`],
    failed: ['Connection failed', host],
  }[connection]);
  const pages = [{ id: 'classes', label: 'Classes', icon: 'classes' }, { id: 'settings', label: 'Settings', icon: 'gear' }];
</script>
<aside class="sidebar">
  <nav aria-label="Main navigation">
    <button type="button" class="item" aria-haspopup="dialog" onclick={onConnection}>
      <Icon name="server" size={26} /><span class="label">Server connection</span><span class="dot {connection}" aria-hidden="true"></span>
    </button>
    {#each pages as page (page.id)}
      <button type="button" class="item" class:active={connected && section === page.id} disabled={!connected} aria-current={connected && section === page.id ? 'page' : undefined} title={connected ? undefined : 'Connect to a server first'} onclick={() => onSelect(page.id)}>
        <Icon name={page.icon} size={26} /><span class="label">{page.label}</span>{#if !connected}<Icon name="lock" size={16} class="lock" />{/if}
      </button>
    {/each}
    <button type="button" class="item" onclick={onQuit}><Icon name="power" size={26} /><span class="label">Close</span></button>
  </nav>
  <div class="status" role="group" aria-live="polite" aria-label="Connection status">
    <span class="dot {connection}" aria-hidden="true"></span>
    <div><strong>{status[0]}</strong>{#if status[1]}<small>{status[1]}</small>{/if}</div>
  </div>
</aside>
<style>
  .sidebar { box-sizing: border-box; min-height: 100%; height: 100%; display: flex; flex-direction: column; justify-content: space-between; gap: var(--space-4); padding: var(--space-5) var(--space-3) var(--space-4); color: var(--nav-text); background: var(--sidebar); }
  nav { display: grid; gap: .35rem; }
  .item { position: relative; display: flex; align-items: center; gap: .95rem; width: 100%; min-height: 3.4rem; margin: 0; padding: .7rem 1rem; border: 0; border-radius: 12px; background: var(--transparent); color: var(--nav-text); font-size: 1.08rem; font-weight: 500; text-align: left; }
  .item:hover:not(:disabled) { background: var(--sidebar-raised); border-color: var(--transparent); color: var(--sidebar-ink); }
  .item:active:not(:disabled) { background: var(--sidebar-raised); }
  .item.active, .item.active:hover:not(:disabled) { background: var(--nav-active); color: var(--sidebar-ink); font-weight: 600; box-shadow: inset 4px 0 0 var(--nav-accent); }
  .item:disabled { cursor: not-allowed; background: var(--transparent); border-color: var(--transparent); color: var(--nav-muted); opacity: .55; }
  .item:focus-visible { outline: 3px solid var(--nav-accent); outline-offset: -3px; }
  .label { flex: 1; min-width: 0; overflow-wrap: anywhere; }
  .item :global(.lock) { flex: none; opacity: .9; }
  .status { display: flex; align-items: flex-start; gap: .65rem; padding: .8rem 1rem; border-radius: 12px; background: var(--sidebar-status); font-size: var(--text-sm); }
  .status strong { display: block; color: var(--sidebar-ink); font-weight: 600; }
  .status small { display: block; color: var(--nav-muted); overflow-wrap: anywhere; }
  .status .dot { margin-top: .35rem; }
  .dot { flex: none; width: .65rem; height: .65rem; border-radius: 50%; background: var(--sidebar-ink); box-shadow: 0 0 0 2px var(--sidebar-dot-halo); }
  .dot.connected { background: var(--ok-on-dark); }
  .dot.connecting { background: var(--sidebar-ink); }
  .dot.lost { background: var(--sand); }
  .dot.connecting { animation: pulse 1.1s ease-in-out infinite; }
  .dot.failed { background: var(--coral-tint); }
  @keyframes pulse { 50% { opacity: .55; } }
  @media (max-width: 820px) {
    .sidebar { flex-direction: row; flex-wrap: wrap; align-items: center; padding: var(--space-2) var(--space-3); }
    nav { display: flex; flex-wrap: wrap; gap: .25rem; }
    .item { width: auto; min-height: 2.8rem; padding: .45rem .8rem; font-size: 1rem; }
    .item.active, .item.active:hover:not(:disabled) { box-shadow: inset 0 -3px 0 var(--nav-accent); }
    .status { flex: 1 1 100%; align-items: center; padding: .35rem .7rem; }
    .status .dot { margin-top: 0; }
    .status div { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0 .6rem; }
    .status small { margin: 0; }
  }
</style>

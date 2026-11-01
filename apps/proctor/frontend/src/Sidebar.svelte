<script>
  import Icon from '../../../../shared/theme/Icon.svelte';
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
  .sidebar { box-sizing: border-box; min-height: 100%; height: 100%; display: flex; flex-direction: column; justify-content: space-between; gap: var(--space-4); padding: 1.6rem .75rem .75rem; color: var(--side-text); background: var(--side-bg); }
  nav { display: grid; gap: .3rem; }
  .item { position: relative; display: flex; align-items: center; gap: .9rem; width: 100%; min-height: 3.2rem; margin: 0; padding: .75rem .85rem; border: 0; border-radius: 9px; background: var(--transparent); color: var(--side-text); font-size: .98rem; font-weight: 500; text-align: left; box-shadow: none; }
  /* The page icon is dim at rest and turns cyan on hover and on the current page. */
  .item > :global(svg:first-child) { flex: none; color: var(--side-text-dim); transition: color .15s ease; }
  .item:hover:not(:disabled) { background: var(--side-hover); border-color: var(--transparent); color: var(--side-text); }
  .item:hover:not(:disabled) > :global(svg:first-child) { color: var(--accent-cyan-soft); }
  .item:active:not(:disabled) { background: var(--side-active); box-shadow: none; }
  .item.active, .item.active:hover:not(:disabled) { background: var(--side-active); color: var(--on-dark); font-weight: 600; }
  .item.active > :global(svg:first-child) { color: var(--accent-cyan-soft); }
  .item.active::before { content: ''; position: absolute; left: -.75rem; top: 18%; bottom: 18%; width: 4px; border-radius: 0 4px 4px 0; background: var(--accent-bar-side); }
  .item:disabled { cursor: not-allowed; background: var(--transparent); border-color: var(--transparent); color: var(--side-text-dim); opacity: .55; }
  .item:focus-visible { outline: 2px solid var(--accent-cyan-soft); outline-offset: -2px; box-shadow: none; }
  .label { flex: 1; min-width: 0; overflow-wrap: anywhere; }
  .item :global(.lock) { flex: none; opacity: .9; }
  .status { display: flex; align-items: flex-start; gap: .6rem; padding: .7rem .9rem; border: 1px solid var(--side-border); border-radius: 10px; background: var(--side-hover); font-size: .85rem; }
  .status strong { display: block; color: var(--on-dark); font-weight: 600; }
  /* The server address is monospace; the sentence shown before any connection is not an address. */
  .status small { display: block; margin-top: .1rem; color: var(--side-text-dim); font-family: var(--font-mono); font-size: .8rem; overflow-wrap: anywhere; }
  .status:has(.idle) small { font-family: inherit; }
  .status .dot { margin-top: .4rem; }
  .dot { flex: none; width: 9px; height: 9px; border-radius: 50%; background: var(--muted-dot); }
  .dot.connected { background: var(--ok); box-shadow: 0 0 0 3px var(--ok-glow); }
  .dot.connecting { background: var(--accent-cyan); animation: pulse 1.1s ease-in-out infinite; }
  .dot.lost { background: var(--warn); }
  .dot.failed { background: var(--alert); }
  @keyframes pulse { 50% { opacity: .65; } }
  @media (max-width: 820px) {
    .sidebar { flex-direction: row; flex-wrap: wrap; align-items: center; padding: var(--space-2) var(--space-3); }
    nav { display: flex; flex-wrap: wrap; gap: .25rem; }
    .item { width: auto; min-height: 2.8rem; padding: .45rem .8rem; font-size: 1rem; }
    .item.active::before { display: none; }
    .item.active, .item.active:hover:not(:disabled) { box-shadow: inset 0 -3px 0 var(--accent-cyan-soft); }
    .status { flex: 1 1 100%; align-items: center; padding: .35rem .7rem; }
    .status .dot { margin-top: 0; }
    .status div { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0 .6rem; }
    .status small { margin: 0; }
  }
</style>

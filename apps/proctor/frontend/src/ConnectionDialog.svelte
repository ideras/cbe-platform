<script>
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import { message } from './errors.js';
  // Where the proctor says which server to use and which token file proves who they are. The token is read by the native
  // side and never reaches this window; only the file's name does.
  let { view, onClose, onConnected, onChanged, onDisconnected } = $props();
  // svelte-ignore state_referenced_locally
  let server = $state(view.server ?? '');
  let busy = $state(false);
  let urlProblem = $state('');
  let tokenProblem = $state('');
  let problem = $state('');
  const working = $derived(busy || view.connecting);
  const tokenKnown = $derived(!!view.credential_file || view.credential_available);
  const host = $derived((() => { try { return new URL(view.server).host; } catch { return view.server; } })());
  const suggestions = $derived([...new Set([...(view.known_servers ?? []), 'http://127.0.0.1:8080'])]);

  function checkAddress(value) {
    let url;
    try { url = new URL(value.trim()); } catch { return 'Enter the server address, for example http://192.168.1.10:8080.'; }
    if (url.protocol !== 'http:' || !url.hostname) return 'Use an http:// address with a host, for example http://192.168.1.10:8080.';
    if (url.username || url.password || (url.pathname !== '/' && url.pathname !== '') || url.search || url.hash) return 'Give only the address and port, without a path, user name or query.';
    return '';
  }
  async function browse() {
    problem = ''; tokenProblem = '';
    try { const choice = await api.ChooseCredential(); if (!choice.cancelled) await onChanged(); }
    catch (err) { tokenProblem = message(err); }
  }
  async function disconnect() {
    problem = ''; busy = true;
    try { onDisconnected(await api.Disconnect()); }
    catch (err) { problem = message(err); }
    finally { busy = false; }
  }
  async function connect() {
    problem = '';
    urlProblem = checkAddress(server);
    tokenProblem = tokenKnown ? '' : 'Choose the token file the server was started with (its -admin-token-file).';
    if (urlProblem || tokenProblem) return;
    busy = true;
    try { const value = await api.Connect(server.trim()); onConnected(value); }
    catch (err) { problem = message(err); }
    finally { busy = false; }
  }
</script>
<Modal title="Server connection" titleId="connection-title" busy={working} {onClose}>
  <form onsubmit={event => { event.preventDefault(); connect(); }} novalidate>
    <p class="lead">Connect to the exam server on your network. The token file proves you are the proctor; it is read here and never shown.</p>
    {#if view.connected}<p class="connected"><span class="dot" aria-hidden="true"></span><span>Connected to <strong>{host}</strong>. Connecting to another address replaces this connection.</span></p>{/if}
    <label for="connection-server">Server address</label>
    <input id="connection-server" bind:value={server} list="connection-servers" inputmode="url" autocomplete="off" spellcheck="false" placeholder="http://192.168.1.10:8080" disabled={working} aria-invalid={urlProblem ? 'true' : undefined} aria-describedby={urlProblem ? 'connection-server-problem' : undefined} oninput={() => urlProblem = ''} />
    <datalist id="connection-servers">{#each suggestions as address (address)}<option value={address}></option>{/each}</datalist>
    {#if urlProblem}<p class="problem" id="connection-server-problem">{urlProblem}</p>{/if}

    <div class="label" id="connection-token-label">Token file</div>
    <div class="token" role="group" aria-labelledby="connection-token-label">
      <span class="file" class:chosen={view.credential_file}>{view.credential_file ? `${view.credential_file} · loaded, not shown` : view.credential_available ? 'Given when the dashboard started' : 'No token file chosen'}</span>
      <button type="button" disabled={working} onclick={browse}><Icon name="folder" size={18} />{view.credential_file ? 'Choose another…' : 'Browse…'}</button>
    </div>
    {#if tokenProblem}<p class="problem">{tokenProblem}</p>{/if}
    {#if view.notice}<p class="callout warning">{view.notice}</p>{/if}
    <small class="hint">Use the file the server reads with <code>-admin-token-file</code>; it must be readable only by you. HTTP is for a trusted isolated network.</small>

    {#if problem}<p role="alert">{problem}</p>{/if}
    <div class="actions">
      {#if view.connected}<button type="button" class="disconnect" disabled={working} onclick={disconnect}>Disconnect</button>{/if}
      <button type="button" disabled={working} onclick={onClose}>Cancel</button>
      <button type="submit" class="primary" disabled={working}>{#if working}<span class="spinner" aria-hidden="true"></span>Connecting…{:else}Connect{/if}</button>
    </div>
  </form>
</Modal>
<style>
  .lead { margin-top: 0; color: var(--color-text-muted); }
  label, .label { display: block; margin: var(--space-4) 0 var(--space-1); color: var(--title-ink); font-weight: 600; }
  input { width: 100%; }
  .token { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2) var(--space-3); padding: .45rem .6rem; background: var(--color-control-bg); border: 1px solid var(--color-control-border); border-radius: var(--radius-control); }
  .file { flex: 1 1 10rem; min-width: 0; overflow-wrap: anywhere; color: var(--color-text-muted); }
  .file.chosen { color: var(--color-text); }
  .token button { display: inline-flex; align-items: center; gap: .4rem; margin: 0; }
  .problem { margin: var(--space-1) 0 0; color: var(--color-danger); font-size: var(--text-sm); font-weight: 600; }
  .hint { display: block; margin-top: var(--space-2); color: var(--color-text-muted); }
  .connected { display: flex; align-items: center; gap: .55rem; margin: var(--space-3) 0 0; padding: .5rem .7rem; color: var(--color-success); background: var(--color-success-bg); border-radius: var(--radius-control); font-size: var(--text-sm); }
  .connected strong { color: inherit; }
  .dot { flex: none; width: .6rem; height: .6rem; border-radius: 50%; background: var(--ok); }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-5); }
  .disconnect { margin-right: auto; border-color: var(--color-danger); color: var(--color-danger); }
  .disconnect:hover:not(:disabled) { background: var(--color-danger-bg); border-color: var(--color-danger-strong); color: var(--color-danger-strong); }
  .actions button { margin: 0; min-width: 6.5rem; }
  .spinner { display: inline-block; width: .95rem; height: .95rem; margin-right: .5rem; vertical-align: -.15rem; border: 2px solid var(--spinner-track); border-top-color: var(--ink-2); border-radius: 50%; animation: spin .8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>

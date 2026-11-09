<script>
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { message } from './errors.js';
  // Where the proctor says which server to use and which token file proves who they are (owner's mock server-connection-dialog.html,
  // 2026-11-08). The token is read by the native side and never reaches this window; only the file's name does.
  let { view, onClose, onConnected, onChanged, onDisconnected } = $props();
  // svelte-ignore state_referenced_locally
  let server = $state(view.server ?? '');
  let busy = $state(false);
  let urlProblem = $state('');
  let tokenProblem = $state('');
  let problem = $state('');
  const working = $derived(busy || view.connecting);
  const tokenKnown = $derived(!!view.credential_file || view.credential_available);
  const hostOf = address => { try { return new URL(address).host; } catch { return address; } };
  const host = $derived(hostOf(view.server));
  const suggestions = $derived([...new Set([...(view.known_servers ?? []), 'http://127.0.0.1:8080'])]);

  // What the typed address is, as it is typed: its scheme, and whether it is the server already connected.
  const scheme = $derived(/^(https?):\/\/[^\s/]+/i.exec(server.trim())?.[1].toLowerCase() ?? '');
  const comparable = address => address.trim().replace(/\/+$/, '').toLowerCase();
  const same = $derived(view.connected && comparable(server) === comparable(view.server ?? ''));
  const currentScheme = $derived(/^https:/i.test(view.server ?? '') ? 'https' : 'http');
  // A note under the address: that connecting there replaces the connection, and that http is not encrypted.
  const note = $derived.by(() => {
    if (!scheme) return null;
    const replaces = view.connected && !same;
    if (replaces && scheme === 'http') return { tone: 'warn', icon: 'warning', lead: 'Unencrypted, and replaces the connection to ', host, tail: '. Use HTTP only on a trusted, isolated network.' };
    if (replaces) return { tone: 'switch', icon: 'arrowRight', lead: 'Connecting here replaces the connection to ', host, tail: '.' };
    if (scheme === 'http') return { tone: 'warn', icon: 'warning', lead: 'Unencrypted. Use HTTP only on a trusted, isolated network.', host: '', tail: '' };
    return null;
  });
  const action = $derived(!view.connected ? 'Connect' : same ? 'Reconnect' : 'Switch server');

  function checkAddress(value) {
    let url;
    try { url = new URL(value.trim()); } catch { return 'Enter the server address, for example http://192.168.1.10:8080.'; }
    if ((url.protocol !== 'http:' && url.protocol !== 'https:') || !url.hostname) return 'Use an http or https address with a host, for example http://192.168.1.10:8080.';
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
<Modal title="Server connection" titleId="connection-title" tone="deep" icon="server" subtitle="The exam server on your network" width="35rem" busy={working} {onClose}>
  <form onsubmit={event => { event.preventDefault(); connect(); }} novalidate>
    <div class="content">
      <div class="current" class:on={view.connected} class:pending={!view.connected && view.connecting} role="status">
        <span class="state-dot" aria-hidden="true"></span>
        <div class="current-text">
          <span class="k">{view.connected ? 'Connected' : view.connecting ? 'Connecting' : 'Not connected'}</span>
          <span class="v">{view.connected || view.connecting ? host : 'No server'}</span>
        </div>
        {#if view.connected}<span class="pill {currentScheme}">{currentScheme.toUpperCase()}</span>{/if}
      </div>

      <div class="field">
        <label for="connection-server">Server address</label>
        <div class="address" class:bad={urlProblem}>
          <span class="scheme-mark {scheme}" aria-hidden="true"><Icon name={scheme === 'https' ? 'lock' : scheme === 'http' ? 'lockOpen' : 'server'} size={16} /></span>
          <input id="connection-server" bind:value={server} list="connection-servers" inputmode="url" autocomplete="off" spellcheck="false" placeholder="http://192.168.1.10:8080" disabled={working}
            aria-invalid={urlProblem ? 'true' : undefined} aria-describedby={urlProblem ? 'connection-server-problem' : note ? 'connection-server-note' : undefined} oninput={() => urlProblem = ''} />
          {#if scheme}<span class="scheme {scheme}" aria-hidden="true">{scheme.toUpperCase()}</span>{/if}
        </div>
        <datalist id="connection-servers">{#each suggestions as address (address)}<option value={address}></option>{/each}</datalist>
        {#if urlProblem}<p class="msg bad" id="connection-server-problem">{urlProblem}</p>
        {:else if note}<p class="msg {note.tone}" id="connection-server-note"><Icon name={note.icon} size={14} /><span>{note.lead}{#if note.host}<strong>{note.host}</strong>{/if}{note.tail}</span></p>{/if}
      </div>

      <div class="field">
        <div class="label" id="connection-token-label">Token file <span>· proves you are the proctor</span></div>
        <div class="token" role="group" aria-labelledby="connection-token-label">
          <span class="token-mark" aria-hidden="true"><Icon name="shieldCheck" size={17} /></span>
          <div class="token-info">
            {#if view.credential_file}<span class="path" title={view.credential_file}>{'‎' + view.credential_file + '‎'}</span>
            {:else}<span class="path none">{view.credential_available ? 'Given when the dashboard started' : 'No token file chosen'}</span>{/if}
            {#if tokenKnown}
              <span class="tags"><span class="tag ok"><Icon name="check" size={11} />Loaded</span><span class="tag quiet"><Icon name="eyeOff" size={11} />Read here, never shown</span></span>
            {/if}
          </div>
          <button type="button" class="choose" disabled={working} onclick={browse}><Icon name="folder" size={16} />{view.credential_file ? 'Choose another…' : 'Browse…'}</button>
        </div>
        {#if tokenProblem}<p class="msg bad">{tokenProblem}</p>{/if}
      </div>

      <p class="help"><Icon name="info" size={15} /><span>Use the same file the server reads with <code>-admin-token-file</code>. It must be readable only by you.</span></p>
      {#if view.notice}<p class="callout warning">{view.notice}</p>{/if}
      {#if working}<p class="progress"><span class="spinner" aria-hidden="true"></span>Connecting to {hostOf(server.trim()) || 'the server'}…</p>{/if}
      {#if problem}<p role="alert">{problem}</p>{/if}
    </div>
    <div class="foot">
      {#if view.connected}<button type="button" class="disconnect" disabled={working} onclick={disconnect}><Icon name="power" size={16} />Disconnect</button>{/if}
      <span class="spacer"></span>
      <button type="button" class="cancel" disabled={working} onclick={onClose}>Cancel</button>
      <button type="submit" class="primary" disabled={working}>{#if working}<span class="spinner" aria-hidden="true"></span>Connecting…{:else}<Icon name="wifi" size={16} />{action}{/if}</button>
    </div>
  </form>
</Modal>
<style>
  .content { display: grid; gap: var(--space-4); padding: 1.1rem; }

  .current { display: flex; flex-wrap: wrap; align-items: center; gap: .75rem; padding: .75rem .9rem; color: var(--text-muted); background: var(--bg-primary); border: 1px solid var(--border-subtle); border-left: 5px solid var(--muted-dot); border-radius: var(--radius); }
  .current .state-dot { flex: none; width: 11px; height: 11px; border-radius: 50%; background: var(--muted-dot); }
  .current-text { display: grid; min-width: 0; }
  .k { font-size: .68rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .v { font-family: var(--font-mono); font-size: .92rem; font-weight: 700; overflow-wrap: anywhere; }
  .current.on { color: var(--ok-text); background: var(--ok-fill); border-color: var(--ok-border); border-left-color: var(--ok); }
  .current.on .state-dot { background: var(--ok); animation: okpulse 1.8s infinite; }
  .current.pending { color: var(--action-hover); background: var(--hover-fill); border-color: var(--action-border); border-left-color: var(--action-primary); }
  .current.pending .state-dot { background: var(--action-primary); }
  @keyframes okpulse { 0% { box-shadow: 0 0 0 0 var(--ok-glow); } 70% { box-shadow: 0 0 0 8px var(--transparent); } 100% { box-shadow: 0 0 0 0 var(--transparent); } }
  .pill { margin-left: auto; padding: .08rem .5rem; border-radius: 999px; background: var(--bg-raised); border: 1px solid var(--ok-border); color: var(--ok-text); font-family: var(--font-mono); font-size: .75rem; font-weight: 700; }
  .pill.http { border-color: var(--warn-border); color: var(--warn-text); }

  .field { display: grid; gap: .35rem; }
  label, .label { display: block; margin: 0; color: var(--text-main); font-size: .88rem; font-weight: 600; }
  .label span { color: var(--text-muted); font-size: .82rem; font-weight: 400; }

  .address { display: flex; align-items: center; gap: var(--space-2); height: 44px; padding: 0 .4rem 0 .7rem; background: var(--bg-primary); border: 1px solid var(--control-border); border-radius: 9px; transition: border-color .2s, box-shadow .2s; }
  .address:focus-within { border-color: var(--action-primary); box-shadow: 0 0 0 3px var(--focus-halo); }
  .address.bad { border-color: var(--alert); box-shadow: 0 0 0 3px var(--alert-halo); }
  .scheme-mark { display: grid; place-items: center; width: 22px; color: var(--muted-dot); }
  .scheme-mark.https { color: var(--ok-text); }
  .scheme-mark.http { color: var(--warn-text); }
  .address input { flex: 1; min-width: 0; min-height: 0; margin: 0; padding: 0; border: 0; background: var(--transparent); box-shadow: none; font-family: var(--font-mono); font-size: .95rem; color: var(--text-main); }
  /* The box draws the focus, so the field's own ring is off (the global rule is more specific than a plain override). */
  .address input:not([type="checkbox"]):not([type="radio"]):focus-visible { border: 0; box-shadow: none; outline: none; }
  .scheme { padding: .12rem .5rem; border-radius: 999px; font-family: var(--font-mono); font-size: .72rem; font-weight: 700; letter-spacing: .04em; white-space: nowrap; }
  .scheme.https { background: var(--ok-fill); color: var(--ok-text); }
  .scheme.http { background: var(--warn-fill); color: var(--warn-text); }
  .msg { display: flex; align-items: flex-start; gap: .4rem; margin: 0; color: var(--text-muted); font-size: .8rem; }
  .msg :global(svg) { margin-top: .12rem; }
  .msg.warn { color: var(--warn-text); }
  .msg.switch { color: var(--action-hover); }
  .msg.bad { color: var(--alert-text); font-weight: 600; }
  .msg strong { color: inherit; }

  .token { display: flex; flex-wrap: wrap; align-items: center; gap: .7rem; padding: .6rem .6rem .6rem .75rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); }
  .token-mark { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 9px; background: var(--selection-fill); color: var(--action-primary); }
  .token-info { display: grid; gap: .2rem; flex: 1 1 12rem; min-width: 0; }
  /* A long path keeps its end, the file's name, in view. */
  .path { overflow: hidden; color: var(--text-main); font-family: var(--font-mono); font-size: .85rem; font-weight: 600; white-space: nowrap; text-overflow: ellipsis; direction: rtl; text-align: left; }
  .path.none { direction: ltr; color: var(--text-muted); font-family: inherit; font-weight: 400; }
  .tags { display: flex; flex-wrap: wrap; gap: .35rem; }
  .tag { display: inline-flex; align-items: center; gap: .3rem; padding: .05rem .5rem; border-radius: 999px; font-size: .72rem; font-weight: 600; }
  .tag.ok { background: var(--ok-fill); color: var(--ok-text); }
  .tag.quiet { background: var(--bg-secondary); color: var(--text-main); }
  .choose { display: inline-flex; align-items: center; gap: .4rem; min-height: 34px; margin: 0; padding: 0 .8rem; background: var(--bg-raised); border-color: var(--action-border); color: var(--action-hover); font-size: .86rem; }

  .help { display: flex; align-items: flex-start; gap: var(--space-2); margin: 0; padding: .5rem .7rem; color: var(--text-muted); background: var(--bg-primary); border: 1px dashed var(--border-subtle); border-radius: 9px; font-size: .8rem; }
  .help :global(svg) { margin-top: .12rem; color: var(--action-primary); }
  .help code { padding: 0 .3rem; border-radius: 4px; background: var(--bg-secondary); color: var(--text-main); font-family: var(--font-mono); font-size: .78rem; }
  .callout { margin: 0; }
  .progress { display: flex; align-items: center; gap: var(--space-2); margin: 0; padding: .6rem .75rem; color: var(--action-hover); background: var(--hover-fill); border: 1px solid var(--action-border); border-radius: 9px; font-size: .88rem; }
  .content > [role="alert"] { margin: 0; }

  .foot { position: sticky; bottom: 0; display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; padding: .85rem 1.1rem; background: var(--bg-primary); border-top: 1px solid var(--border-subtle); }
  .spacer { flex: 1; }
  .foot button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; margin: 0; min-width: 6.5rem; }
  .cancel:not(:disabled) { background: var(--transparent); color: var(--text-muted); }
  .cancel:hover:not(:disabled) { background: var(--bg-secondary); border-color: var(--border-subtle); color: var(--text-main); }
  .disconnect:not(:disabled) { background: var(--bg-raised); border-color: var(--alert-border); color: var(--alert-text); }
  .disconnect:hover:not(:disabled) { background: var(--alert-fill); border-color: var(--alert); color: var(--alert-text); }
  .spinner { display: inline-block; flex: none; width: .95rem; height: .95rem; border: 2px solid var(--spinner-track); border-top-color: var(--action-primary); border-radius: 50%; animation: spin .8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (max-width: 520px) {
    .spacer { display: none; }
    .foot button { flex: 1; }
    .choose { flex: 1 1 100%; justify-content: center; }
  }
</style>

<script>
  import { onMount, onDestroy, tick, untrack } from 'svelte';
  import { EditorState } from '@codemirror/state';
  import { EditorView } from '@codemirror/view';
  import Icon from '../../../../../shared/theme/Icon.svelte';
  import { editorTheme } from '../editor-theme.js';
  import { basename, dirname } from './paths.js';
  import { ZOOMS, DEFAULT_ZOOM, stepZoom } from './sizes.js';
  import { isSvgPath, looksLikeSvg } from './picture.js';
  import { access, sizing, sizeTheme, accessConfig } from './editor-store.js';
  import PictureView from './PictureView.svelte';
  import MenuButton from './MenuButton.svelte';

  // A pane of the editor: the tabs of the files open in it, and one CodeMirror editor that shows the state of the tab in front. There are
  // one or two panes; what each tab is (its editor state, with its undo history and its cursor, where it was scrolled, and an SVG's picture)
  // is kept in the store the panes share (editor-store.js), so it survives a trip to another tab and a move to the other pane. The editor
  // tells the model when the text changes; the model decides when it is written. Highlighting only, as in the exam's code answers.
  //
  // `tabs` are this pane's, in order, and `allTabs` every open file's (two tabs with the same name say which folder they are in, in whichever
  // pane). `side` is '' with one pane and 'left' or 'right' with two; `used` says this is the pane last used, whose tab in front is marked.
  // `onClose` closes one tab, `onCloseMany` several (the ⋯ menu's Close all, Close others and Close saved), and `onMove` sends one to the
  // other pane.
  let { model, store, paneId, tabs, allTabs = tabs, activePath, readOnly, side = '', used = true, canMove = false,
    onClose, onCloseMany = () => {}, onMove = () => {} } = $props();
  let host = $state(null);
  let view;
  // The size of the code is part of the editor's theme, and a change of it is dispatched as a change of the theme. CodeMirror measures its
  // lines (the height it believes each has, which moving the cursor up and down and finding the line under the mouse go by) when its
  // content changes height or its theme changes, and in no other case: a size that came from the page's CSS alone (the toolbar's A− and
  // A+, ours below) left it with the old heights when the code did not fill the editor, and the arrow keys then skipped lines.
  function resize() { view?.dispatch({ effects: sizing.reconfigure(sizeTheme()) }); }
  let shownId = $state(null);
  // With two panes every name says which pane it is in.
  const named = text => (side ? `${text} (${side})` : text);

  const active = $derived(tabs.find(tab => tab.path === activePath) ?? null);
  const showing = $derived(active?.state === 'ready');
  let picture = $state(null);
  const label = tab => allTabs.some(other => other.id !== tab.id && basename(other.path) === basename(tab.path)) ? `${basename(tab.path)} (${dirname(tab.path) || '/'})` : basename(tab.path);

  // The size of the code text, on top of the application's A− and A+ (which size the whole workspace, files and buttons too): a student
  // who wants bigger code need not make everything else bigger, which would take the room the code needs. Ctrl and + − 0 do it from the
  // keyboard, as in a code editor. Each pane has its own. It is kept while the application runs and is not written anywhere.
  let zoomIndex = $state(DEFAULT_ZOOM);
  const zoom = delta => { zoomIndex = stepZoom(zoomIndex, delta); };
  $effect(() => { zoomIndex; if (view) tick().then(resize); });
  // A click leaves the focus where the student was working (the editor); a key press on the button keeps it on the button.
  const zoomClick = (delta, event) => { zoom(delta); if (event.detail > 0) view?.focus(); };

  // The tab in front changes: the pane lets go of the one that was, and shows the state of the one that is (taken from the other pane when
  // the tab was just moved from there).
  let wanted = 0;
  async function show() {
    const ticket = ++wanted;
    const tab = untrack(() => active);
    if (shownId !== null && view) { store.release(shownId, view); shownId = null; }
    if (!tab || tab.state !== 'ready') return;
    await store.ensure(tab);
    if (ticket !== wanted || !view) return;
    view.setState(store.take(tab.id, view));
    const scroll = store.scrolls.get(tab.id);
    view.dispatch({ effects: [access.reconfigure(accessConfig(readOnly)), sizing.reconfigure(sizeTheme()), ...(scroll ? [scroll] : [])] });
    shownId = tab.id;
  }
  // What decides it are the tab's id and state, read through values of their own: `active` is a new object at every snapshot of the model
  // (a save, a tick, a terminal's output), and an effect that read it would redraw the whole editor and lose its scroll each time.
  const activeId = $derived(active?.id ?? null);
  const activeState = $derived(active?.state ?? null);
  // Where the tab going to the back is scrolled is read before the page changes: a tab in front that is not text hides the editor, and a
  // hidden editor has no scroll to read.
  $effect.pre(() => { activeId; activeState; untrack(() => { if (shownId !== null && view && store.mine(shownId, view)) store.scrolls.set(shownId, view.scrollSnapshot()); }); });
  $effect(() => { activeId; activeState; if (view) untrack(show); });

  // An SVG file is shown as its picture, or as its source in the editor (docs/workspace/student-ide.md, "Editor"). The tab is the same
  // tab either way: its text, its saves and its conflicts are the model's, as for any file, and the editor keeps its state (cursor,
  // scroll, undo) behind the picture. Which of the two a tab shows is the student's choice, kept in the store while the tab is open, with
  // where its picture was (its zoom and its scroll), for when the tab comes to the front again: the picture is made anew for it.
  const views = store.views;
  const placeOf = id => { if (!store.pictures.has(id)) store.pictures.set(id, {}); return store.pictures.get(id); };
  const activeSvg = $derived(activePath !== null && isSvgPath(activePath));
  const activeVersion = $derived(active?.version ?? '');
  const activeSaved = $derived(active?.savedText ?? '');
  const activeDirty = $derived(active?.dirty ?? false);
  const activeView = $derived(activeId === null ? undefined : views.get(activeId));
  // What the picture draws: the editor's text, so that edits not yet saved show too; it is read again when the file changes (a dot that
  // made it again, a reload), when it is saved and when the view changes, and from the tab's primitives only, never at every snapshot.
  const pictureText = $derived.by(() => {
    if (!activeSvg || activeState !== 'ready') return '';
    const id = activeId;
    const saved = activeSaved;
    activeVersion; activeDirty; activeView;
    return untrack(() => model.buffers.get(id)?.text() ?? saved);
  });
  const pictureLike = $derived(looksLikeSvg(pictureText));
  // A file that looks like an SVG opens as a picture, and stays one when it is half written for a moment.
  const pictureShown = $derived(activeSvg && activeState === 'ready' && (activeView === 'picture' || (activeView === undefined && pictureLike)));
  const choosing = $derived(activeSvg && activeState === 'ready' && (pictureLike || activeView === 'picture'));
  $effect(() => { if (pictureShown && untrack(() => !views.has(activeId))) views.set(activeId, 'picture'); });

  // Showing the other view puts the keyboard in it.
  async function choose(mode) {
    if (activeId === null) return;
    views.set(activeId, mode);
    await tick();
    if (mode === 'picture') picture?.focus(); else view?.focus();
  }

  $effect(() => { const locked = readOnly; if (view) view.dispatch({ effects: access.reconfigure(accessConfig(locked)) }); });

  onMount(() => {
    view = new EditorView({ parent: host, state: EditorState.create({ doc: '', extensions: [editorTheme('0'), sizing.of(sizeTheme())] }) });
    store.zooms.set(view, zoom);
    // Where the tab shown is scrolled is kept as it changes: a pane that goes (the other is left alone) is taken off the page before it
    // could be asked, and an editor off the page reads as scrolled to the top.
    view.scrollDOM.addEventListener('scroll', () => { if (shownId !== null && view && store.mine(shownId, view)) store.scrolls.set(shownId, view.scrollSnapshot()); }, { passive: true });
    show();
    window.addEventListener('exam-text-size', resize);
    return () => window.removeEventListener('exam-text-size', resize);
  });
  // A pane that goes (the other one is the only one left) leaves the tab it showed in the store, where it was.
  onDestroy(() => {
    if (!view) return;
    if (shownId !== null) store.release(shownId, view);
    store.zooms.delete(view);
    view.destroy();
    view = null;
  });

  // The keyboard goes to what the tab shows: its picture, or the editor. The pane is drawn from the model's snapshots, which come once a
  // frame: a file just opened from the tree is not in it yet. When a picture is in front, or may be about to be, the keyboard goes where
  // the next snapshot puts it (else it would go into an editor about to be hidden behind a picture, and from there to nowhere).
  export function focus() {
    const front = model.panes.find(pane => pane.id === paneId)?.active ?? null;
    if (!pictureShown && !isSvgPath(front ?? '')) { view?.focus(); return; }
    requestAnimationFrame(() => tick().then(() => { if (pictureShown) picture?.focus(); else view?.focus(); }));
  }

  // The tab with the keyboard is the one in front (the arrows bring the next one to the front), so Ctrl+W, which the page takes wherever
  // the keyboard is, closes it too.
  function tabKeys(event, index) {
    const move = delta => { event.preventDefault(); const next = tabs[(index + delta + tabs.length) % tabs.length]; if (next) { model.activate(next.path); queueMicrotask(() => document.getElementById(`file-tab-${next.id}`)?.focus()); } };
    if (event.key === 'ArrowRight') move(1);
    else if (event.key === 'ArrowLeft') move(-1);
    else if (event.key === 'Home') { event.preventDefault(); move(-index); }
    else if (event.key === 'End') { event.preventDefault(); move(tabs.length - 1 - index); }
    else if (event.key === 'Delete') { event.preventDefault(); onClose(tabs[index].path); }
  }
  // A middle click closes a tab, as in a browser or a code editor (its press is kept from starting the scroll mode some systems give it).
  const middle = event => event.button === 1;

  // The ⋯ menu: Close all, Close others (all but the one in front) and Close saved (the tabs with no unsaved changes) of this pane, with how
  // many each closes; then, for the tab in front, Move to the other side and Close with its key. Each tab is saved before it closes.
  const shortcut = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform || navigator.userAgent) ? '⌘W' : 'Ctrl+W';
  const others = $derived(tabs.filter(tab => tab.path !== activePath).map(tab => tab.path));
  const saved = $derived(tabs.filter(tab => !tab.dirty).map(tab => tab.path));
  const otherSide = $derived(side === 'right' ? 'left' : 'right');
  const tabActions = $derived([
    { label: 'Close all', detail: tabs.length, disabled: tabs.length === 0, run: () => onCloseMany(tabs.map(tab => tab.path)) },
    { label: 'Close others', detail: others.length, disabled: others.length === 0, run: () => onCloseMany(others) },
    { label: 'Close saved', detail: saved.length, disabled: saved.length === 0, run: () => onCloseMany(saved) },
    'separator',
    { label: active ? `Move ${label(active)} to the ${otherSide}` : `Move to the ${otherSide}`, disabled: !active || !canMove, run: () => { if (active) onMove(active.path); } },
    { label: active ? `Close ${label(active)}` : 'Close', detail: shortcut, disabled: !active, run: () => { if (active) onClose(active.path); } },
  ]);
</script>
<section class="editor" class:used aria-label={named('Editor')} style:--code-zoom={ZOOMS[zoomIndex]}>
  <div class="tabbar">
    <div class="tabs" role="tablist" aria-label={named('Open files')}>
      {#each tabs as tab, index (tab.id)}
        <button type="button" role="tab" class="tab" class:front={tab.path === activePath} id="file-tab-{tab.id}" aria-selected={tab.path === activePath} aria-controls="editor-panel-{paneId}"
          tabindex={tab.path === activePath ? 0 : -1} title={tab.path} onclick={() => model.activate(tab.path)} onkeydown={event => tabKeys(event, index)}
          onmousedown={event => { if (middle(event)) event.preventDefault(); }} onauxclick={event => { if (middle(event)) { event.preventDefault(); onClose(tab.path); } }}>
          {label(tab)}<span class="mark">{#if tab.dirty}<span class="dot" role="img" aria-label="unsaved changes">●</span>{/if}</span>
          <!-- The mouse's ways to close a tab are this and a middle click; the keyboard's are Delete on the tab, Ctrl+W anywhere but a terminal,
               and the ⋯ menu and the button next to the tabs. -->
          <span class="close" aria-hidden="true" onclick={event => { event.stopPropagation(); onClose(tab.path); }}><Icon name="close" size={14} /></span>
        </button>
      {/each}
      {#if tabs.length === 0}<span class="none">No file open. Open one from the file tree.</span>{/if}
    </div>
    {#if choosing}
      <div class="views" role="group" aria-label="Show {basename(active.path)} as">
        <button type="button" aria-pressed={pictureShown} onclick={() => choose('picture')}>Picture</button>
        <button type="button" aria-pressed={!pictureShown} onclick={() => choose('source')}>Source</button>
      </div>
    {/if}
    <!-- The names start with what is drawn on the buttons, as WCAG's "label in name" asks (a person who says "A plus" to a voice control must reach it).
         They size the code text, so a picture, which has its own zoom, does not show them. -->
    {#if !pictureShown}
      <div class="zoom" role="group" aria-label={named('Code text size')}>
        <button type="button" aria-label="A− Smaller code text" title="Smaller code text (Ctrl and −)" disabled={zoomIndex === 0} onclick={event => zoomClick(-1, event)}>A−</button>
        <button type="button" aria-label="A+ Larger code text" title="Larger code text (Ctrl and +)" disabled={zoomIndex === ZOOMS.length - 1} onclick={event => zoomClick(1, event)}>A+</button>
      </div>
    {/if}
    <!-- The tab in front goes to the other pane (made when there is one); with one pane and one tab there is nowhere to send it. -->
    <button type="button" class="move" aria-label={named('Move to the other side')} title={active ? `Move ${label(active)} to the ${otherSide}` : 'Move a tab to the other side'}
      disabled={!active || !canMove} onclick={() => { if (active) onMove(active.path); }}><Icon name="split" size={16} /></button>
    <MenuButton label={named('Tab actions')} items={tabActions} disabled={tabs.length === 0} />
    {#if active}<button type="button" class="close-active" aria-label="Close {label(active)}" title="Close this file ({shortcut})" onclick={() => onClose(active.path)}><Icon name="close" size={14} /></button>{/if}
  </div>
  <div class="panel" id="editor-panel-{paneId}" role="tabpanel" aria-labelledby={active ? `file-tab-${active.id}` : undefined}>
    {#if active?.conflict}
      <div class="notice conflict" role="alert">
        <span>{#if active.conflict.content === null}<strong>{basename(active.path)}</strong> changed in the terminal and cannot be shown here.{:else}This file changed in the terminal.{/if} Reload it, or keep yours?</span>
        <span class="choices">
          <button type="button" class="primary" onclick={() => model.resolveConflict(active.path, 'keep')} disabled={readOnly}>Keep yours</button>
          <button type="button" onclick={() => model.resolveConflict(active.path, 'reload')} disabled={active.conflict.content === null || readOnly}>Reload</button>
        </span>
      </div>
    {:else if active?.missing && active.state === 'ready'}
      <div class="notice" role="status">This file no longer exists. Your text is kept: save it (Ctrl+S) to put the file back.</div>
    {/if}
    <!-- The picture and the editor lie one over the other, and the one not shown keeps its place (the editor its scroll, the picture its zoom),
         out of sight and out of reach. -->
    <div class="stage" hidden={!showing}>
      <div class="host" class:behind={pictureShown} bind:this={host}></div>
      {#if choosing || pictureShown}
        {#key activeId}
          <div class="front" class:behind={!pictureShown}><PictureView bind:this={picture} name={basename(activePath)} text={pictureText} place={placeOf(activeId)} onSource={() => choose('source')} /></div>
        {/key}
      {/if}
    </div>
    {#if active && active.state === 'loading'}<p class="message" role="status">Opening {basename(active.path)}…</p>{/if}
    {#if active && ['binary', 'toolarge', 'error'].includes(active.state)}<p class="message" role="status">{active.message}</p>{/if}
    {#if !active}<p class="message">Open a file from the file tree to edit it. Changes are saved as you type.</p>{/if}
  </div>
</section>
<style>
  .editor { display: grid; grid-template-rows: auto minmax(0, 1fr); min-height: 0; min-width: 0; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); overflow: hidden; }
  .tabbar { display: flex; align-items: stretch; background: var(--bg-secondary); border-bottom: 1px solid var(--border-subtle); min-height: 2.1rem; }
  .tabs { display: flex; flex: 1 1 auto; align-items: stretch; gap: 1px; min-width: 0; overflow-x: auto; }
  .tab { display: inline-flex; align-items: center; flex: none; gap: .25rem; margin: 0; padding: .35rem .5rem .35rem .7rem; border: 0; border-bottom: 3px solid var(--transparent); border-radius: 0; background: var(--bg-secondary); box-shadow: none; color: var(--text-main); font-family: var(--font-mono); font-size: var(--text-sm); }
  .tab.front { background: var(--bg-raised); border-bottom-color: var(--action-primary); font-weight: 700; }
  .zoom, .views { display: flex; flex: none; align-items: stretch; }
  .views { border-left: 1px solid var(--border-subtle); border-right: 1px solid var(--border-subtle); }
  .views button[aria-pressed="true"] { background: var(--bg-raised); box-shadow: inset 0 -3px 0 var(--action-primary); }
  .zoom button, .views button { margin: 0; padding: .25rem .5rem; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); font-size: var(--text-sm); font-weight: 700; }
  .zoom button:disabled { opacity: .45; }
  .zoom button:focus-visible, .views button:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .tab:focus-visible, .close-active:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .close { display: inline-flex; padding: .1rem; border-radius: 4px; color: var(--text-muted); }
  .close:hover { color: var(--alert-text); background: var(--hover-fill); }
  .close-active, .move { flex: none; margin: 0; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); }
  .move { display: inline-flex; align-items: center; padding: .25rem .45rem; }
  .move:disabled { opacity: .45; }
  .move:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  /* With two panes, the tab in front of the pane not in use is marked in grey: the blue one is where the keyboard and the next file go. */
  .editor:not(.used) .tab.front { border-bottom-color: var(--text-muted); }
  .dot { color: var(--warn-text); font-size: .7em; }
  /* The place of the dot is kept when there is none, so the tab does not widen and move the ones after it at every edit and every save. */
  .mark { display: inline-block; width: .8em; text-align: center; }
  .none { padding: .5rem .75rem; color: var(--text-muted); font-size: var(--text-sm); }
  .panel { position: relative; display: flex; flex-direction: column; min-height: 0; }
  .stage { position: relative; flex: 1 1 0; min-height: 0; display: grid; grid-template: minmax(0, 1fr) / minmax(0, 1fr); }
  .stage[hidden] { display: none; }
  .stage > * { grid-area: 1 / 1; min-height: 0; min-width: 0; }
  .host { overflow: hidden; }
  .behind { visibility: hidden; }
  .host :global(.cm-editor) { height: 100%; }
  .host:focus-within { box-shadow: inset 0 0 0 2px var(--focus-halo); }
  .notice { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2); padding: .45rem .75rem; background: var(--warn-fill); color: var(--warn-text); border-bottom: 1px solid var(--border-subtle); font-size: var(--text-sm); }
  .notice.conflict { background: var(--alert-fill); color: var(--alert-text); }
  .choices { display: flex; gap: var(--space-1); }
  .choices button { margin: 0; }
  .message { margin: 0; padding: var(--space-4); color: var(--text-muted); }
</style>

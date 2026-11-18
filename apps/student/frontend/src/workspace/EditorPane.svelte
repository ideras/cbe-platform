<script>
  import { onMount, tick, untrack } from 'svelte';
  import { SvelteMap } from 'svelte/reactivity';
  import { EditorState, Compartment, Annotation } from '@codemirror/state';
  import { EditorView, keymap, lineNumbers, highlightActiveLineGutter, highlightSpecialChars, drawSelection, dropCursor } from '@codemirror/view';
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
  import { foldGutter, foldKeymap, indentOnInput, bracketMatching, syntaxHighlighting } from '@codemirror/language';
  import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
  import Icon from '../../../../../shared/theme/Icon.svelte';
  import { highlightColors, editorTheme } from '../editor-theme.js';
  import { loadLanguage } from '../languages.js';
  import { languageOf, basename, dirname } from './paths.js';
  import { ZOOMS, DEFAULT_ZOOM, stepZoom } from './sizes.js';
  import { isSvgPath, looksLikeSvg } from './picture.js';
  import PictureView from './PictureView.svelte';

  // The open files: a tab for each, and one CodeMirror editor that shows the state of the tab in front. Each tab keeps its own editor
  // state (its undo history and its cursor) and where it was scrolled, so all three survive a trip to another tab. The editor tells the
  // model when the text changes; the model decides when it is written. Highlighting only, as in the exam's code answers: no autocompletion.
  let { model, tabs, activePath, readOnly, onClose } = $props();
  let host = $state(null);
  let view;
  const states = new Map(); // tab id -> the tab's editor state
  // An editor state does not hold the scroll: CodeMirror shows a state given to it from the top. Where a tab was scrolled is kept apart, as
  // CodeMirror's own snapshot of it, and given back with its state.
  const scrolls = new Map(); // tab id -> the snapshot of its scroll when it went to the back
  const extensionsOf = new Map(); // tab id -> the language's extension, for a state made again
  const outside = Annotation.define();
  const access = new Compartment();
  // The size of the code is part of the editor's theme, and a change of it is dispatched as a change of the theme. CodeMirror measures its
  // lines (the height it believes each has, which moving the cursor up and down and finding the line under the mouse go by) when its
  // content changes height or its theme changes, and in no other case: a size that came from the page's CSS alone (the toolbar's A− and
  // A+, ours below) left it with the old heights when the code did not fill the editor, and the arrow keys then skipped lines.
  const sizing = new Compartment();
  const sizeTheme = () => EditorView.theme({ '&': { fontSize: 'calc(var(--text-sm) * var(--code-zoom, 1))' } });
  function resize() { view?.dispatch({ effects: sizing.reconfigure(sizeTheme()) }); }
  let shownId = $state(null);
  const buffers = new Map(); // tab id -> the buffer registered with the model

  const active = $derived(tabs.find(tab => tab.path === activePath) ?? null);
  const showing = $derived(active?.state === 'ready');
  let picture = $state(null);
  // Two tabs with the same name say which folder they are in.
  const label = tab => tabs.some(other => other.id !== tab.id && basename(other.path) === basename(tab.path)) ? `${basename(tab.path)} (${dirname(tab.path) || '/'})` : basename(tab.path);

  // The size of the code text, on top of the application's A− and A+ (which size the whole workspace, files and buttons too): a student
  // who wants bigger code need not make everything else bigger, which would take the room the code needs. Ctrl and + − 0 do it from the
  // keyboard, as in a code editor. It is kept while the application runs and is not written anywhere.
  let zoomIndex = $state(DEFAULT_ZOOM);
  const zoom = delta => { zoomIndex = stepZoom(zoomIndex, delta); };
  $effect(() => { zoomIndex; if (view) tick().then(resize); });
  // A click leaves the focus where the student was working (the editor); a key press on the button keeps it on the button.
  const zoomClick = (delta, event) => { zoom(delta); if (event.detail > 0) view?.focus(); };

  const accessConfig = locked => [EditorState.readOnly.of(locked), EditorView.contentAttributes.of(locked ? { 'aria-readonly': 'true' } : {})];

  function makeState(tab, text, language) {
    return EditorState.create({ doc: text, extensions: [
      lineNumbers(), highlightActiveLineGutter(), highlightSpecialChars(), history(), foldGutter(),
      drawSelection(), dropCursor(), indentOnInput(), bracketMatching(), closeBrackets(), highlightSelectionMatches(),
      syntaxHighlighting(highlightColors), language,
      keymap.of([
        { key: 'Mod-s', preventDefault: true, run: () => { model.save(model.tabs.find(item => item.id === tab.id)?.path ?? tab.path).catch(() => {}); return true; } },
        { key: 'Mod-=', run: () => { zoom(1); return true; } }, { key: 'Mod-+', run: () => { zoom(1); return true; } },
        { key: 'Mod--', run: () => { zoom(-1); return true; } }, { key: 'Mod-0', run: () => { zoom(0); return true; } },
        indentWithTab, ...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap,
      ]),
      EditorView.contentAttributes.of({ 'aria-label': `Editor for ${tab.path}`, spellcheck: 'false' }),
      access.of(accessConfig(readOnly)),
      editorTheme('0'),
      sizing.of(sizeTheme()),
      EditorView.updateListener.of(update => {
        if (update.docChanged && !update.transactions.some(t => t.annotation(outside))) {
          model.editedById(tab.id);
          // A student typing an SVG of their own stays in its source when the text starts to look like one.
          if (!views.has(tab.id)) views.set(tab.id, 'source');
        }
      }),
    ] });
  }

  async function ensure(tab) {
    let state = states.get(tab.id);
    if (state) return state;
    const language = await loadLanguage(languageOf(tab.path)).catch(() => []);
    state = states.get(tab.id) ?? makeState(tab, tab.savedText, language);
    states.set(tab.id, state);
    extensionsOf.set(tab.id, language);
    if (!buffers.has(tab.id)) {
      const id = tab.id;
      const buffer = {
        text: () => (shownId === id ? view.state.doc.toString() : states.get(id)?.doc.toString() ?? ''),
        replace: text => {
          if (shownId === id) view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text }, annotations: outside.of(true) });
          else {
            states.set(id, makeState({ id, path: model.tabs.find(item => item.id === id)?.path ?? tab.path }, text, extensionsOf.get(id) ?? []));
            scrolls.delete(id); // a snapshot is of the text it was taken in, and this is another
          }
        },
      };
      buffers.set(tab.id, buffer);
      model.registerBuffer(tab.path, buffer);
    }
    return state;
  }

  // The tab in front changes: keep the state of the one that was, show the state of the one that is.
  let wanted = 0;
  async function show() {
    const ticket = ++wanted;
    const tab = untrack(() => active);
    if (shownId !== null && view) states.set(shownId, view.state);
    if (!tab || tab.state !== 'ready') { shownId = null; return; }
    const state = await ensure(tab);
    if (ticket !== wanted) return;
    view.setState(state);
    const scroll = scrolls.get(tab.id);
    view.dispatch({ effects: [access.reconfigure(accessConfig(readOnly)), sizing.reconfigure(sizeTheme()), ...(scroll ? [scroll] : [])] });
    shownId = tab.id;
  }
  // What decides it are the tab's id and state, read through values of their own: `active` is a new object at every snapshot of the model
  // (a save, a tick, a terminal's output), and an effect that read it would redraw the whole editor and lose its scroll each time.
  const activeId = $derived(active?.id ?? null);
  const activeState = $derived(active?.state ?? null);
  // Where the tab going to the back is scrolled is read before the page changes: a tab in front that is not text hides the editor, and a
  // hidden editor has no scroll to read.
  $effect.pre(() => { activeId; activeState; untrack(() => { if (shownId !== null && view) scrolls.set(shownId, view.scrollSnapshot()); }); });
  $effect(() => { activeId; activeState; if (view) untrack(show); });

  // An SVG file is shown as its picture, or as its source in the editor (docs/workspace/student-ide.md, "Editor"). The tab is the same
  // tab either way: its text, its saves and its conflicts are the model's, as for any file, and the editor keeps its state (cursor,
  // scroll, undo) behind the picture. Which of the two a tab shows is the student's choice, kept here while the tab is open.
  const views = new SvelteMap(); // tab id -> 'picture' | 'source'
  // Where each tab's picture was (its zoom and its scroll), for when the tab comes to the front again: the picture is made anew for it.
  const pictures = new Map(); // tab id -> { zoom, left, top }
  const placeOf = id => { if (!pictures.has(id)) pictures.set(id, {}); return pictures.get(id); };
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

  // What the model keeps are the tabs that exist: the state of one that was closed goes with it.
  $effect(() => {
    const ids = new Set(tabs.map(tab => tab.id));
    for (const id of untrack(() => [...views.keys()])) if (!ids.has(id)) views.delete(id);
    for (const map of [scrolls, pictures]) for (const id of [...map.keys()]) if (!ids.has(id)) map.delete(id);
    for (const id of [...states.keys()]) if (!ids.has(id)) {
      states.delete(id); extensionsOf.delete(id);
      const buffer = buffers.get(id);
      if (buffer) { model.unregisterBuffer(id, buffer); buffers.delete(id); }
    }
  });

  $effect(() => { const locked = readOnly; if (view) view.dispatch({ effects: access.reconfigure(accessConfig(locked)) }); });

  onMount(() => {
    view = new EditorView({ parent: host, state: EditorState.create({ doc: '', extensions: [editorTheme('0'), sizing.of(sizeTheme())] }) });
    show();
    window.addEventListener('exam-text-size', resize);
    return () => { window.removeEventListener('exam-text-size', resize); view.destroy(); view = null; };
  });

  // The keyboard goes to what the tab shows: its picture, or the editor. The pane is drawn from the model's snapshots, which come once a
  // frame: a file just opened from the tree is not in it yet. When a picture is in front, or may be about to be, the keyboard goes where
  // the next snapshot puts it (else it would go into an editor about to be hidden behind a picture, and from there to nowhere).
  export function focus() {
    if (!pictureShown && !isSvgPath(model.activePath ?? '')) { view?.focus(); return; }
    requestAnimationFrame(() => tick().then(() => { if (pictureShown) picture?.focus(); else view?.focus(); }));
  }

  function tabKeys(event, index) {
    const move = delta => { event.preventDefault(); const next = tabs[(index + delta + tabs.length) % tabs.length]; if (next) { model.activate(next.path); queueMicrotask(() => document.getElementById(`file-tab-${next.id}`)?.focus()); } };
    if (event.key === 'ArrowRight') move(1);
    else if (event.key === 'ArrowLeft') move(-1);
    else if (event.key === 'Home') { event.preventDefault(); move(-index); }
    else if (event.key === 'End') { event.preventDefault(); move(tabs.length - 1 - index); }
    else if (event.key === 'Delete' || (event.key === 'w' && (event.ctrlKey || event.metaKey))) { event.preventDefault(); onClose(tabs[index].path); }
  }
</script>
<section class="editor" aria-label="Editor" style:--code-zoom={ZOOMS[zoomIndex]}>
  <div class="tabbar">
    <div class="tabs" role="tablist" aria-label="Open files">
      {#each tabs as tab, index (tab.id)}
        <button type="button" role="tab" class="tab" class:front={tab.path === activePath} id="file-tab-{tab.id}" aria-selected={tab.path === activePath} aria-controls="editor-panel"
          tabindex={tab.path === activePath ? 0 : -1} title={tab.path} onclick={() => model.activate(tab.path)} onkeydown={event => tabKeys(event, index)}>
          {label(tab)}<span class="mark">{#if tab.dirty}<span class="dot" role="img" aria-label="unsaved changes">●</span>{/if}</span>
          <!-- The mouse's way to close a tab; the keyboard's are Delete and Ctrl+W on the tab, and the button next to the tabs. -->
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
      <div class="zoom" role="group" aria-label="Code text size">
        <button type="button" aria-label="A− Smaller code text" title="Smaller code text (Ctrl and −)" disabled={zoomIndex === 0} onclick={event => zoomClick(-1, event)}>A−</button>
        <button type="button" aria-label="A+ Larger code text" title="Larger code text (Ctrl and +)" disabled={zoomIndex === ZOOMS.length - 1} onclick={event => zoomClick(1, event)}>A+</button>
      </div>
    {/if}
    {#if active}<button type="button" class="close-active" aria-label="Close {label(active)}" title="Close this file" onclick={() => onClose(active.path)}><Icon name="close" size={14} /></button>{/if}
  </div>
  <div class="panel" id="editor-panel" role="tabpanel" aria-labelledby={active ? `file-tab-${active.id}` : undefined}>
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
  .close-active { flex: none; margin: 0; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); }
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

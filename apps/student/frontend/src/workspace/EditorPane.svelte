<script>
  import { onMount, untrack } from 'svelte';
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

  // The open files: a tab for each, and one CodeMirror editor that shows the state of the tab in front. Each tab keeps its own editor
  // state, so its undo history, its cursor and its scroll survive a trip to another tab. The editor tells the model when the text
  // changes; the model decides when it is written. Highlighting only, as in the exam's code answers: no autocompletion.
  let { model, tabs, activePath, readOnly, onClose } = $props();
  let host = $state(null);
  let view;
  const states = new Map(); // tab id -> the tab's editor state
  const extensionsOf = new Map(); // tab id -> the language's extension, for a state made again
  const outside = Annotation.define();
  const access = new Compartment();
  let shownId = $state(null);
  const buffers = new Map(); // tab id -> the buffer registered with the model

  const active = $derived(tabs.find(tab => tab.path === activePath) ?? null);
  const showing = $derived(active?.state === 'ready');
  // Two tabs with the same name say which folder they are in.
  const label = tab => tabs.some(other => other.id !== tab.id && basename(other.path) === basename(tab.path)) ? `${basename(tab.path)} (${dirname(tab.path) || '/'})` : basename(tab.path);

  const accessConfig = locked => [EditorState.readOnly.of(locked), EditorView.contentAttributes.of(locked ? { 'aria-readonly': 'true' } : {})];

  function makeState(tab, text, language) {
    return EditorState.create({ doc: text, extensions: [
      lineNumbers(), highlightActiveLineGutter(), highlightSpecialChars(), history(), foldGutter(),
      drawSelection(), dropCursor(), indentOnInput(), bracketMatching(), closeBrackets(), highlightSelectionMatches(),
      syntaxHighlighting(highlightColors), language,
      keymap.of([
        { key: 'Mod-s', preventDefault: true, run: () => { model.save(model.tabs.find(item => item.id === tab.id)?.path ?? tab.path).catch(() => {}); return true; } },
        indentWithTab, ...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap,
      ]),
      EditorView.contentAttributes.of({ 'aria-label': `Editor for ${tab.path}`, spellcheck: 'false' }),
      access.of(accessConfig(readOnly)),
      editorTheme('0'),
      EditorView.updateListener.of(update => {
        if (update.docChanged && !update.transactions.some(t => t.annotation(outside))) model.editedById(tab.id);
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
          else states.set(id, makeState({ id, path: model.tabs.find(item => item.id === id)?.path ?? tab.path }, text, extensionsOf.get(id) ?? []));
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
    view.dispatch({ effects: access.reconfigure(accessConfig(readOnly)) });
    shownId = tab.id;
  }
  $effect(() => { active?.id; active?.state; if (view) untrack(show); });

  // What the model keeps are the tabs that exist: the state of one that was closed goes with it.
  $effect(() => {
    const ids = new Set(tabs.map(tab => tab.id));
    for (const id of [...states.keys()]) if (!ids.has(id)) {
      states.delete(id); extensionsOf.delete(id);
      const buffer = buffers.get(id);
      if (buffer) { model.unregisterBuffer(id, buffer); buffers.delete(id); }
    }
  });

  $effect(() => { const locked = readOnly; if (view) view.dispatch({ effects: access.reconfigure(accessConfig(locked)) }); });

  onMount(() => {
    view = new EditorView({ parent: host, state: EditorState.create({ doc: '', extensions: [editorTheme('0')] }) });
    show();
    const measure = () => view.requestMeasure();
    window.addEventListener('exam-text-size', measure);
    return () => { window.removeEventListener('exam-text-size', measure); view.destroy(); view = null; };
  });

  export function focus() { view?.focus(); }

  function tabKeys(event, index) {
    const move = delta => { event.preventDefault(); const next = tabs[(index + delta + tabs.length) % tabs.length]; if (next) { model.activate(next.path); queueMicrotask(() => document.getElementById(`file-tab-${next.id}`)?.focus()); } };
    if (event.key === 'ArrowRight') move(1);
    else if (event.key === 'ArrowLeft') move(-1);
    else if (event.key === 'Home') { event.preventDefault(); move(-index); }
    else if (event.key === 'End') { event.preventDefault(); move(tabs.length - 1 - index); }
    else if (event.key === 'Delete' || (event.key === 'w' && (event.ctrlKey || event.metaKey))) { event.preventDefault(); onClose(tabs[index].path); }
  }
</script>
<section class="editor" aria-label="Editor">
  <div class="tabbar">
    <div class="tabs" role="tablist" aria-label="Open files">
      {#each tabs as tab, index (tab.id)}
        <button type="button" role="tab" class="tab" class:front={tab.path === activePath} id="file-tab-{tab.id}" aria-selected={tab.path === activePath} aria-controls="editor-panel"
          tabindex={tab.path === activePath ? 0 : -1} title={tab.path} onclick={() => model.activate(tab.path)} onkeydown={event => tabKeys(event, index)}>
          {label(tab)}{#if tab.dirty}<span class="dot" role="img" aria-label="unsaved changes">●</span>{/if}
          <!-- The mouse's way to close a tab; the keyboard's are Delete and Ctrl+W on the tab, and the button next to the tabs. -->
          <span class="close" aria-hidden="true" onclick={event => { event.stopPropagation(); onClose(tab.path); }}><Icon name="close" size={14} /></span>
        </button>
      {/each}
      {#if tabs.length === 0}<span class="none">No file open. Open one from the file tree.</span>{/if}
    </div>
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
    <div class="host" bind:this={host} hidden={!showing}></div>
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
  .tab:focus-visible, .close-active:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .close { display: inline-flex; padding: .1rem; border-radius: 4px; color: var(--text-muted); }
  .close:hover { color: var(--alert-text); background: var(--hover-fill); }
  .close-active { flex: none; margin: 0; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); }
  .dot { color: var(--warn-text); font-size: .7em; }
  .none { padding: .5rem .75rem; color: var(--text-muted); font-size: var(--text-sm); }
  .panel { position: relative; display: flex; flex-direction: column; min-height: 0; }
  .host { flex: 1 1 0; min-height: 0; overflow: hidden; }
  .host :global(.cm-editor) { height: 100%; font-size: var(--text-sm); }
  .host:focus-within { box-shadow: inset 0 0 0 2px var(--focus-halo); }
  .notice { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2); padding: .45rem .75rem; background: var(--warn-fill); color: var(--warn-text); border-bottom: 1px solid var(--border-subtle); font-size: var(--text-sm); }
  .notice.conflict { background: var(--alert-fill); color: var(--alert-text); }
  .choices { display: flex; gap: var(--space-1); }
  .choices button { margin: 0; }
  .message { margin: 0; padding: var(--space-4); color: var(--text-muted); }
</style>

<script>
  import { onMount } from 'svelte';
  import { foldGutter, foldKeymap, indentOnInput, bracketMatching, syntaxHighlighting } from '@codemirror/language';
  import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
  import { highlightColors, editorTheme } from './editor-theme.js';
  import { loadLanguage } from './languages.js';
  import { Compartment, EditorState } from '@codemirror/state';
  import { indentWithTab, defaultKeymap, history, historyKeymap } from '@codemirror/commands';
  import { EditorView, keymap, lineNumbers, highlightActiveLineGutter, highlightSpecialChars, drawSelection, dropCursor, rectangularSelection, crosshairCursor } from '@codemirror/view';

  // Highlighting only: never install autocompletion or its keymap in an exam.
  let { slotId, language = '', rows = 0, value, locked, onChange } = $props();
  let host;
  let editor = $state(null);
  const access = new Compartment();
  const syntax = new Compartment();
  let synchronizing = false;

  onMount(() => {
    const id = slotId;
    const label = language ? `${language} code answer for ${id}` : `Code answer for ${id}`;
    editor = new EditorView({
      parent: host,
      state: EditorState.create({ doc: value, extensions: [
        lineNumbers(), highlightActiveLineGutter(), highlightSpecialChars(), history(), foldGutter(),
        drawSelection(), dropCursor(), EditorState.allowMultipleSelections.of(true),
        indentOnInput(), bracketMatching(), closeBrackets(), rectangularSelection(), crosshairCursor(),
        highlightSelectionMatches(), syntaxHighlighting(highlightColors), syntax.of([]),
        keymap.of([indentWithTab, ...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap]),
        EditorView.lineWrapping,
        EditorView.contentAttributes.of({ 'aria-label': label }),
        access.of([EditorState.readOnly.of(locked), EditorView.editable.of(!locked)]),
        EditorView.updateListener.of(update => {
          if (update.docChanged && !synchronizing) onChange(id, update.state.doc.toString());
        }),
        editorTheme(`${(rows || 12) * 1.5}rem`),
      ] }),
    });
    const mounted = editor;
    // An editor may be destroyed during the import; never dispatch into its replacement.
    loadLanguage(language).then(extension => { if (editor === mounted) mounted.dispatch({ effects: syntax.reconfigure(extension) }); })
      .catch(() => { /* Plain text remains usable if a bundled parser cannot load. */ });
    const measure = () => editor?.requestMeasure();
    window.addEventListener('exam-text-size', measure);
    return () => { window.removeEventListener('exam-text-size', measure); editor.destroy(); editor = null; };
  });

  $effect(() => {
    if (!editor) return;
    editor.dispatch({ effects: access.reconfigure([EditorState.readOnly.of(locked), EditorView.editable.of(!locked)]) });
    if (editor.state.doc.toString() !== value) {
      synchronizing = true;
      try { editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: value } }); }
      finally { synchronizing = false; }
    }
  });
</script>

<div class="editor" bind:this={host}>{#if language}<span class="language">{language}</span>{/if}</div>

<style>
  .editor { position: relative; margin: var(--space-2) 0; border: 1px solid var(--control-border); border-radius: var(--radius-ctl); overflow: hidden; transition: border-color .15s ease, box-shadow .15s ease; }
  .language { position: absolute; top: .25rem; right: .5rem; z-index: 1; color: var(--text-muted); font-size: var(--text-xs); text-transform: uppercase; letter-spacing: .05em; pointer-events: none; }
  .editor:focus-within { border-color: var(--action-primary); box-shadow: 0 0 0 3px var(--focus-halo); }
</style>

<script>
  import { onMount } from 'svelte';
  import { foldGutter, foldKeymap, indentOnInput, bracketMatching, syntaxHighlighting, HighlightStyle } from '@codemirror/language';
  import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
  import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
  import { tags } from '@lezer/highlight';
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
  const colors = HighlightStyle.define([
    { tag: tags.keyword, color: 'var(--syntax-keyword)', fontWeight: 'bold' },
    { tag: [tags.string, tags.character], color: 'var(--syntax-string)' },
    { tag: [tags.number, tags.bool, tags.null], color: 'var(--syntax-number)' },
    { tag: tags.comment, color: 'var(--syntax-comment)', fontStyle: 'italic' },
    { tag: tags.variableName, color: 'var(--syntax-variable)' },
    { tag: [tags.labelName, tags.function(tags.variableName)], color: 'var(--syntax-label)' },
    { tag: [tags.typeName, tags.meta], color: 'var(--syntax-type)' },
  ]);
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
        highlightSelectionMatches(), syntaxHighlighting(colors), syntax.of([]),
        keymap.of([indentWithTab, ...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap]),
        EditorView.lineWrapping,
        EditorView.contentAttributes.of({ 'aria-label': label }),
        access.of([EditorState.readOnly.of(locked), EditorView.editable.of(!locked)]),
        EditorView.updateListener.of(update => {
          if (update.docChanged && !synchronizing) onChange(id, update.state.doc.toString());
        }),
        EditorView.theme({
          // Like every field: the canvas color at rest, white while focused; the wrapper draws the focus ring.
          '&': { backgroundColor: 'var(--bg-primary)', color: 'var(--text-main)', minHeight: `${(rows || 12) * 1.5}rem` },
          '&.cm-focused': { outline: 'none', backgroundColor: 'var(--paper)' },
          '.cm-content': { fontFamily: 'var(--font-mono)', caretColor: 'var(--text-main)' },
          '.cm-content:focus-visible': { outline: 'none', boxShadow: 'none' },
          '.cm-scroller': { overflow: 'auto' },
          '.cm-gutters': { backgroundColor: 'var(--transparent)', color: 'var(--text-muted)', borderColor: 'var(--border-subtle)' },
          // The cursor's line is told by its number. Its text is never given a fill of its own: the selection is drawn in a layer
          // behind the text, so any background on a line would cover a selection made on that line.
          '.cm-activeLineGutter': { backgroundColor: 'var(--transparent)', color: 'var(--action-hover)', fontWeight: '700' },
          '.cm-panels': { backgroundColor: 'var(--bg-raised)', color: 'var(--text-muted)', borderColor: 'var(--border-subtle)' },
          '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, & > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-content ::selection': { backgroundColor: 'var(--selection-fill)', color: 'var(--text-main)' },
          '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--text-main)' },
          '.cm-tooltip': { backgroundColor: 'var(--bg-raised)', color: 'var(--text-main)', borderColor: 'var(--control-border)' },
          '.cm-tooltip-arrow:before': { borderTopColor: 'var(--control-border)', borderBottomColor: 'var(--control-border)' },
          '.cm-tooltip-arrow:after': { borderTopColor: 'var(--bg-raised)', borderBottomColor: 'var(--bg-raised)' },
          '.cm-panels-top, .cm-panels-bottom': { borderColor: 'var(--border-subtle)' },
          '.cm-placeholder': { color: 'var(--text-muted)' },
          '.cm-selectionMatch, .cm-searchMatch': { backgroundColor: 'var(--selection-fill)', color: 'var(--text-main)' },
          '.cm-searchMatch-selected': { backgroundColor: 'var(--selection-fill)', outline: '1px solid var(--action-primary)' },
          '&.cm-focused .cm-matchingBracket': { backgroundColor: 'var(--selection-fill)', color: 'var(--action-hover)' },
          '&.cm-focused .cm-nonmatchingBracket': { backgroundColor: 'var(--alert-fill)', color: 'var(--alert-text)' },
          '.cm-specialChar': { color: 'var(--alert-text)' },
          '.cm-trailingSpace': { backgroundColor: 'var(--alert-fill)' },
          '.cm-highlightTab': { backgroundImage: 'radial-gradient(circle at 50% 55%, var(--text-muted) 20%, var(--transparent) 0)' },
          '.cm-foldPlaceholder': { backgroundColor: 'var(--hover-fill)', color: 'var(--text-muted)', borderColor: 'var(--control-border)' },
          '.cm-button': { backgroundImage: 'none', backgroundColor: 'var(--bg-secondary)', color: 'var(--text-main)', borderColor: 'var(--border-subtle)' },
          '.cm-button:hover, .cm-button:active': { backgroundImage: 'none', backgroundColor: 'var(--selection-fill)' },
          '.cm-textfield': { backgroundColor: 'var(--bg-primary)', color: 'var(--text-main)', borderColor: 'var(--control-border)' },
          '.cm-button:focus-visible, .cm-textfield:focus-visible': { outline: '2px solid var(--action-primary)' },
        }, { dark: false }),
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

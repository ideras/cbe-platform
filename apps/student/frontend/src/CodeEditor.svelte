<script>
  import { onMount } from 'svelte';
  import { basicSetup } from 'codemirror';
  import { Compartment, EditorState } from '@codemirror/state';
  import { indentWithTab } from '@codemirror/commands';
  import { EditorView, keymap } from '@codemirror/view';

  // rows sets the visible height; language only names the language for screen readers, without highlighting.
  let { slotId, language = '', rows = 0, value, locked, onChange } = $props();
  let host;
  let editor = $state(null);
  const access = new Compartment();
  let synchronizing = false;

  onMount(() => {
    const id = slotId;
    const label = language ? `${language} code answer for ${id}` : `Code answer for ${id}`;
    editor = new EditorView({
      parent: host,
      state: EditorState.create({ doc: value, extensions: [
        basicSetup,
        keymap.of([indentWithTab]),
        EditorView.lineWrapping,
        EditorView.contentAttributes.of({ 'aria-label': label }),
        access.of([EditorState.readOnly.of(locked), EditorView.editable.of(!locked)]),
        EditorView.updateListener.of(update => {
          if (update.docChanged && !synchronizing) onChange(id, update.state.doc.toString());
        }),
        EditorView.theme({
          '&': { backgroundColor: 'var(--color-surface)', color: 'var(--color-text)', minHeight: `${(rows || 12) * 1.5}rem` },
          '&.cm-focused': { outline: 'none' },
          '.cm-content': { fontFamily: 'var(--font-mono)', caretColor: 'var(--ink)' },
          '.cm-scroller': { overflow: 'auto' },
          '.cm-gutters, .cm-activeLineGutter': { backgroundColor: 'var(--transparent)', color: 'var(--color-text-muted)', borderColor: 'var(--color-border-soft)' },
          '.cm-panels': { backgroundColor: 'var(--color-surface)', color: 'var(--color-text-muted)', borderColor: 'var(--color-border-soft)' },
          '.cm-activeLine': { backgroundColor: 'var(--color-surface-tint)' },
          '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': { backgroundColor: 'var(--selected)', color: 'var(--ink)' },
          '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--ink)' },
          '.cm-tooltip': { backgroundColor: 'var(--surface)', color: 'var(--ink)', borderColor: 'var(--ink-2)' },
          '.cm-tooltip-arrow:before': { borderTopColor: 'var(--ink-2)', borderBottomColor: 'var(--ink-2)' },
          '.cm-tooltip-arrow:after': { borderTopColor: 'var(--surface)', borderBottomColor: 'var(--surface)' },
          '.cm-panels-top, .cm-panels-bottom': { borderColor: 'var(--line)' },
          '.cm-placeholder': { color: 'var(--ink-2)' },
          '.cm-selectionMatch, .cm-searchMatch': { backgroundColor: 'var(--selected)', color: 'var(--ink)' },
          '.cm-searchMatch-selected': { backgroundColor: 'var(--selected)', outline: '1px solid var(--ink-2)' },
          '&.cm-focused .cm-matchingBracket': { backgroundColor: 'var(--sea)', color: 'var(--sea-ink)' },
          '&.cm-focused .cm-nonmatchingBracket': { backgroundColor: 'var(--coral-tint)', color: 'var(--coral)' },
          '.cm-specialChar': { color: 'var(--coral)' },
          '.cm-trailingSpace': { backgroundColor: 'var(--coral-tint)' },
          '.cm-highlightTab': { backgroundImage: 'radial-gradient(circle at 50% 55%, var(--ink-2) 20%, var(--transparent) 0)' },
          '.cm-foldPlaceholder': { backgroundColor: 'var(--hover)', color: 'var(--ink-2)', borderColor: 'var(--ink-2)' },
          '.cm-button': { backgroundImage: 'none', backgroundColor: 'var(--surface)', color: 'var(--ink)', borderColor: 'var(--ink-2)' },
          '.cm-button:hover, .cm-button:active': { backgroundImage: 'none', backgroundColor: 'var(--hover)' },
          '.cm-textfield': { backgroundColor: 'var(--surface)', color: 'var(--ink)', borderColor: 'var(--ink-2)' },
          '.cm-button:focus-visible, .cm-textfield:focus-visible': { outline: 'var(--focus-ring)' },
        }, { dark: false }),
      ] }),
    });
    return () => { editor.destroy(); editor = null; };
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
  .editor { position: relative; margin: var(--space-2) 0; border: 1px solid var(--color-control-border); border-radius: var(--radius-sm); overflow: hidden; }
  .language { position: absolute; top: .25rem; right: .5rem; z-index: 1; color: var(--color-text-muted); font-size: var(--text-xs); text-transform: uppercase; letter-spacing: .05em; pointer-events: none; }
  .editor:focus-within { border-color: var(--ocean); box-shadow: 0 0 0 3px var(--focus-halo); }
</style>

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
          // Like every field: the canvas color at rest, white while focused; the wrapper draws the focus ring.
          '&': { backgroundColor: 'var(--bg-primary)', color: 'var(--text-main)', minHeight: `${(rows || 12) * 1.5}rem` },
          '&.cm-focused': { outline: 'none', backgroundColor: 'var(--paper)' },
          '.cm-content': { fontFamily: 'var(--font-mono)', caretColor: 'var(--text-main)' },
          '.cm-content:focus-visible': { outline: 'none', boxShadow: 'none' },
          '.cm-scroller': { overflow: 'auto' },
          '.cm-gutters, .cm-activeLineGutter': { backgroundColor: 'var(--transparent)', color: 'var(--text-muted)', borderColor: 'var(--border-subtle)' },
          '.cm-panels': { backgroundColor: 'var(--bg-raised)', color: 'var(--text-muted)', borderColor: 'var(--border-subtle)' },
          '.cm-activeLine': { backgroundColor: 'var(--hover-fill)' },
          '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': { backgroundColor: 'var(--selection-fill)', color: 'var(--text-main)' },
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
  .editor { position: relative; margin: var(--space-2) 0; border: 1px solid var(--control-border); border-radius: var(--radius-ctl); overflow: hidden; transition: border-color .15s ease, box-shadow .15s ease; }
  .language { position: absolute; top: .25rem; right: .5rem; z-index: 1; color: var(--text-muted); font-size: var(--text-xs); text-transform: uppercase; letter-spacing: .05em; pointer-events: none; }
  .editor:focus-within { border-color: var(--action-primary); box-shadow: 0 0 0 3px var(--focus-halo); }
</style>

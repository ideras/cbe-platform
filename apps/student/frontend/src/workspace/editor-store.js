import { EditorState, Compartment, Annotation } from '@codemirror/state';
import { EditorView, keymap, lineNumbers, highlightActiveLineGutter, highlightSpecialChars, drawSelection, dropCursor } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { foldGutter, foldKeymap, indentOnInput, bracketMatching, syntaxHighlighting } from '@codemirror/language';
import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
import { SvelteMap } from 'svelte/reactivity';
import { highlightColors, editorTheme } from '../editor-theme.js';
import { loadLanguage } from '../languages.js';
import { languageOf } from './paths.js';

// What the editor panes share about the open files. Each pane has a CodeMirror editor of its own and shows the tab in front of it there;
// what a tab is (its editor state with its undo history and its cursor, where it was scrolled, and for an SVG whether it shows its picture
// or its source, and the picture's zoom and scroll) is kept here, by tab id, so a tab moved to the other pane is where it was left.
//
// The rule that keeps an edit from being lost on the way: a tab's state is the state of the editor that shows it now, or the one kept here
// when none does. A pane that takes a tab the other pane still shows takes its state from that pane's editor (what is kept here may be
// older), and the pane it was taken from sees that the tab is no longer its own and keeps nothing of it.

// A change the model makes to a text (a reload, Keep yours), which is not the student's typing.
export const outside = Annotation.define();
// Whether a text can be changed, and the size of the code: the same compartments in every editor, so a state made in one pane is set up
// again in the other. The size of the code is part of the editor's theme, and a change of it is dispatched as one (see EditorPane).
export const access = new Compartment();
export const sizing = new Compartment();
export const sizeTheme = () => EditorView.theme({ '&': { fontSize: 'calc(var(--text-sm) * var(--code-zoom, 1))' } });
export const accessConfig = locked => [EditorState.readOnly.of(locked), EditorView.contentAttributes.of(locked ? { 'aria-readonly': 'true' } : {})];

export class EditorStore {
  constructor(model) {
    this.model = model;
    this.readOnly = false;
    this.states = new Map(); // tab id -> its state while no editor shows it
    this.scrolls = new Map(); // tab id -> CodeMirror's snapshot of where it was scrolled
    this.languages = new Map(); // tab id -> its language's extension, for a state made again
    this.buffers = new Map(); // tab id -> the buffer registered with the model
    this.showing = new Map(); // tab id -> the EditorView that shows it now
    this.views = new SvelteMap(); // tab id -> 'picture' | 'source'
    this.pictures = new Map(); // tab id -> { zoom, left, top } of its picture
    this.zooms = new WeakMap(); // EditorView -> its pane's zoom(delta): the keys of a state go to the pane that shows it
    this.instructionsScroll = 0; // where the instructions' tab was scrolled, in whichever pane
  }

  // The state of a tab now, and its text.
  state(id) { const view = this.showing.get(id); return view ? view.state : this.states.get(id); }
  text(id) { return this.state(id)?.doc.toString() ?? ''; }
  mine(id, view) { return this.showing.get(id) === view; }

  // A pane is about to show a tab in its editor: the tab's state, taken from the other pane's editor if that one shows it.
  take(id, view) {
    const other = this.showing.get(id);
    if (other && other !== view) {
      this.states.set(id, other.state);
      // (An editor taken off the page reads as scrolled to the top; what it last kept stands then.)
      if (other.dom.isConnected) this.scrolls.set(id, other.scrollSnapshot());
    }
    this.showing.set(id, view);
    return this.states.get(id);
  }
  // A pane stops showing a tab: its state is kept, if it was still this pane's to keep.
  release(id, view) {
    if (!this.mine(id, view)) return;
    this.states.set(id, view.state);
    this.showing.delete(id);
  }

  make(id, path, text, language) {
    const model = this.model;
    const zoom = delta => view => { this.zooms.get(view)?.(delta); return true; };
    return EditorState.create({ doc: text, extensions: [
      lineNumbers(), highlightActiveLineGutter(), highlightSpecialChars(), history(), foldGutter(),
      drawSelection(), dropCursor(), indentOnInput(), bracketMatching(), closeBrackets(), highlightSelectionMatches(),
      syntaxHighlighting(highlightColors), language,
      keymap.of([
        { key: 'Mod-s', preventDefault: true, run: () => { model.save(model.tabs.find(item => item.id === id)?.path ?? path).catch(() => {}); return true; } },
        { key: 'Mod-=', run: zoom(1) }, { key: 'Mod-+', run: zoom(1) }, { key: 'Mod--', run: zoom(-1) }, { key: 'Mod-0', run: zoom(0) },
        indentWithTab, ...closeBracketsKeymap, ...defaultKeymap, ...searchKeymap, ...historyKeymap, ...foldKeymap,
      ]),
      EditorView.contentAttributes.of({ 'aria-label': `Editor for ${path}`, spellcheck: 'false' }),
      access.of(accessConfig(this.readOnly)),
      editorTheme('0'),
      sizing.of(sizeTheme()),
      EditorView.updateListener.of(update => {
        if (update.docChanged && !update.transactions.some(t => t.annotation(outside))) {
          model.editedById(id);
          // A student typing an SVG of their own stays in its source when the text starts to look like one.
          if (!this.views.has(id)) this.views.set(id, 'source');
        }
      }),
    ] });
  }

  // The state of a tab that is about to be shown: made from the text the model read, the first time, with its language.
  async ensure(tab) {
    if (this.state(tab.id)) return;
    const language = await loadLanguage(languageOf(tab.path)).catch(() => []);
    if (!this.state(tab.id)) this.states.set(tab.id, this.make(tab.id, tab.path, tab.savedText, language));
    this.languages.set(tab.id, language);
    if (this.buffers.has(tab.id)) return;
    // The model reads and replaces a tab's text through its buffer, whichever pane shows it, or none.
    const id = tab.id;
    const buffer = {
      text: () => this.text(id),
      replace: text => {
        const view = this.showing.get(id);
        if (view) view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text }, annotations: outside.of(true) });
        else {
          this.states.set(id, this.make(id, this.model.tabs.find(item => item.id === id)?.path ?? tab.path, text, this.languages.get(id) ?? []));
          this.scrolls.delete(id); // a snapshot is of the text it was taken in, and this is another
        }
      },
    };
    this.buffers.set(id, buffer);
    this.model.registerBuffer(tab.path, buffer);
  }

  // What the model keeps are the tabs that exist: everything of a tab that was closed goes with it.
  prune(ids) {
    for (const id of [...this.states.keys(), ...this.showing.keys(), ...this.buffers.keys(), ...this.views.keys(), ...this.pictures.keys(), ...this.scrolls.keys()]) {
      if (ids.has(id)) continue;
      for (const map of [this.states, this.scrolls, this.languages, this.showing, this.pictures]) map.delete(id);
      this.views.delete(id);
      const buffer = this.buffers.get(id);
      if (buffer) { this.model.unregisterBuffer(id, buffer); this.buffers.delete(id); }
    }
  }
}

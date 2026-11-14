import { EditorView } from '@codemirror/view';
import { HighlightStyle } from '@codemirror/language';
import { tags } from '@lezer/highlight';

// The look of every code editor in the application: the highlighting, and the theme in the application's own tokens. Shared by the
// editor of a code answer (CodeEditor.svelte) and the editors of a workspace (workspace/FileEditor.svelte).
export const highlightColors = HighlightStyle.define([
  { tag: tags.keyword, color: 'var(--syntax-keyword)', fontWeight: 'bold' },
  { tag: [tags.string, tags.character], color: 'var(--syntax-string)' },
  { tag: [tags.number, tags.bool, tags.null], color: 'var(--syntax-number)' },
  { tag: tags.comment, color: 'var(--syntax-comment)', fontStyle: 'italic' },
  { tag: tags.variableName, color: 'var(--syntax-variable)' },
  { tag: [tags.labelName, tags.function(tags.variableName)], color: 'var(--syntax-label)' },
  { tag: [tags.typeName, tags.meta], color: 'var(--syntax-type)' },
]);

// minHeight is a CSS length: a code answer is as tall as its rows; a workspace editor fills its panel and passes '0'.
export function editorTheme(minHeight) {
  return EditorView.theme({
    // Like every field: the canvas color at rest, white while focused; the wrapper draws the focus ring.
    '&': { backgroundColor: 'var(--bg-primary)', color: 'var(--text-main)', minHeight: minHeight },
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
  }, { dark: false });
}

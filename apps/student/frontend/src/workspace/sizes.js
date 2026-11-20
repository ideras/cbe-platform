// The sizes the student can choose in the workspace, which are shared by the editor and the terminals so that A− and A+ mean the same in both.

// The steps of the text size, as a multiple of the size the exam is at (the toolbar's A− and A+ come first). The student starts at 1.
export const ZOOMS = [0.85, 1, 1.15, 1.3, 1.5, 1.75, 2];
export const DEFAULT_ZOOM = 1; // an index of ZOOMS
// stepZoom is the index after a step of delta (0 is back to the start), never outside the list.
export const stepZoom = (index, delta) => delta === 0 ? DEFAULT_ZOOM : Math.max(0, Math.min(ZOOMS.length - 1, index + delta));

// The share of the room under the toolbar's row of buttons that the terminals take when they are shown, between the editor and the
// terminals. The editor keeps at least a quarter of it and the terminals at least this much (and 6rem, in the page's CSS).
export const MIN_SHARE = 0.15;
export const MAX_SHARE = 0.75;
export const DEFAULT_SHARE = 0.35;
export const SHARE_STEP = 0.05;
export const clampShare = share => Number.isFinite(share) ? Math.max(MIN_SHARE, Math.min(MAX_SHARE, share)) : DEFAULT_SHARE;

// The share of the workspace's width that the left column (the instructions' entry, the TODO list and the files) takes, once the student has
// moved its splitter. Until then it is null: the usual width of the page's CSS (15 to 21rem), and the editor keeps at least 24rem whatever the share.
export const MIN_SIDE = 0.15;
export const MAX_SIDE = 0.7;
export const SIDE_STEP = 0.025;
export const clampSide = share => Number.isFinite(share) ? Math.max(MIN_SIDE, Math.min(MAX_SIDE, share)) : null;

// The most of that column's height the TODO list takes, above the files: a short list takes only its own room, a long one this much and scrolls.
export const MIN_TODO = 0.1;
export const MAX_TODO = 0.8;
export const DEFAULT_TODO = 0.35;
export const TODO_STEP = 0.05;
export const clampTodo = share => Number.isFinite(share) ? Math.max(MIN_TODO, Math.min(MAX_TODO, share)) : DEFAULT_TODO;

// The left pane's share of the width of the editors when there are two side by side (each keeps at least 16rem, in the page's CSS).
export const MIN_PANE = 0.2;
export const MAX_PANE = 0.8;
export const DEFAULT_PANE = 0.5;
export const PANE_STEP = 0.05;
export const clampPane = share => Number.isFinite(share) ? Math.max(MIN_PANE, Math.min(MAX_PANE, share)) : DEFAULT_PANE;

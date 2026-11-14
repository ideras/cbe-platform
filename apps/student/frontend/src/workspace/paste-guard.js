// Paste control (plan, Phase 4 task 8). Copying and pasting inside the application is allowed, so a student can move code
// between files and terminals; text that comes from outside is not. The guard remembers the last text copied or cut inside the
// application, and lets a paste through only when it is that text. It is a deterrent, like the fullscreen window: it cannot see
// where the clipboard really came from, and it says so to nobody; what it stops is the plain paste.
//
// The line breaks are compared as \n: a clipboard may hand the text back with \r\n.

const normal = text => String(text ?? '').replace(/\r\n?/g, '\n');

export class PasteGuard {
  #last = '';
  noteCopy(text) {
    const value = normal(text);
    if (value !== '') this.#last = value;
  }
  allows(text) {
    const value = normal(text);
    return value !== '' && value === this.#last;
  }
}

// Wires a guard to a window: what is copied or cut anywhere in the page is noted, and a paste or a drop that is not that text, or that
// brings files, is stopped before any editor or terminal sees it. `blocked(kind)` is told each time ("paste" or "drop"). It returns
// the function that takes the listeners off again.
export function guardWindow(win, guard, blocked) {
  const doc = win.document;
  const selected = () => win.getSelection?.()?.toString() ?? '';
  // Bubble phase: an editor that handles the event itself has put what it copies on the clipboard by now.
  const copied = event => guard.noteCopy(event.clipboardData?.getData('text/plain') || selected());
  const dragged = () => guard.noteCopy(selected());
  const stop = event => { event.preventDefault(); event.stopPropagation(); event.stopImmediatePropagation(); };
  const pasted = event => {
    if (guard.allows(event.clipboardData?.getData('text/plain'))) return;
    stop(event);
    blocked('paste');
  };
  const dropped = event => {
    const transfer = event.dataTransfer;
    const files = transfer?.files?.length || [...(transfer?.items ?? [])].some(item => item.kind === 'file');
    if (!files && guard.allows(transfer?.getData('text/plain'))) return;
    stop(event);
    blocked('drop');
  };
  // A drop target is a page that takes the drop: without this the webview would open the dropped file in place of the exam.
  const over = event => event.preventDefault();
  doc.addEventListener('copy', copied);
  doc.addEventListener('cut', copied);
  doc.addEventListener('dragstart', dragged);
  win.addEventListener('paste', pasted, true);
  win.addEventListener('drop', dropped, true);
  win.addEventListener('dragover', over, true);
  return () => {
    doc.removeEventListener('copy', copied);
    doc.removeEventListener('cut', copied);
    doc.removeEventListener('dragstart', dragged);
    win.removeEventListener('paste', pasted, true);
    win.removeEventListener('drop', dropped, true);
    win.removeEventListener('dragover', over, true);
  };
}

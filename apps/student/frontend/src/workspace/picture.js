// An SVG file in the workspace is shown as a picture (docs/workspace/student-ide.md, "Editor"). The rules here are pure, for the tests:
// which files are pictures, how one becomes the address of an image, and the steps of the zoom.
//
// The picture is drawn by an <img> from a data: address, never put into the page: the file is written by the student's own container,
// and an image runs no script and loads nothing from elsewhere. The page's Content-Security-Policy allows data: images and no blob: ones.

// A file whose name ends in .svg, in any case. Not .svgz, which is compressed and so not text.
export const isSvgPath = path => /\.svg$/i.test(String(path ?? ''));

// Whether the text starts as an SVG does: after a byte order mark, white space, the XML declaration and other processing instructions, a
// DOCTYPE and comments, the first element is <svg. A file that only mentions <svg further on (an HTML page, a text about SVG) is not one.
export function looksLikeSvg(text) {
  const source = String(text ?? '');
  let at = source.charCodeAt(0) === 0xfeff ? 1 : 0;
  const skipSpace = () => { while (at < source.length && /\s/.test(source[at])) at += 1; };
  const skipPast = end => { const found = source.indexOf(end, at); at = found < 0 ? source.length : found + end.length; return found >= 0; };
  for (;;) {
    skipSpace();
    if (source.startsWith('<?', at)) { if (!skipPast('?>')) return false; continue; }
    if (source.startsWith('<!--', at)) { if (!skipPast('-->')) return false; continue; }
    if (source.slice(at, at + 9).toUpperCase() === '<!DOCTYPE') {
      // A DOCTYPE may carry declarations of its own between [ and ], which may hold a >.
      const close = source.indexOf('>', at);
      const open = source.indexOf('[', at);
      if (open >= 0 && (close < 0 || open < close)) { at = open; if (!skipPast(']')) return false; }
      if (!skipPast('>')) return false;
      continue;
    }
    break;
  }
  return /^<svg[\s>/]/.test(source.slice(at, at + 5));
}

// The address of the picture: the text, percent-encoded, so that a # (which would end the address), a % and letters such as the ó of
// "Autómata" reach the image as they are. A lone half of a surrogate pair (which encodeURIComponent refuses) becomes U+FFFD. (No
// look-behind in the pattern: an older webview cannot even read a script that has one.)
export function svgDataUri(text) {
  const whole = String(text ?? '').replace(/[\uD800-\uDBFF][\uDC00-\uDFFF]|[\uD800-\uDFFF]/g, unit => (unit.length === 2 ? unit : '\uFFFD'));
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(whole)}`;
}

// The zoom: Fit (the picture as large as the room allows, never larger than its own size), or one of these steps of its own size.
export const FIT = 'fit';
export const PICTURE_ZOOMS = [0.5, 0.75, 1, 1.5, 2, 3, 4];

// The scale a picture of width × height is drawn at to fit a room of roomWidth × roomHeight: down to fit it, never up past 1.
export function fitScale(width, height, roomWidth, roomHeight) {
  if (!(width > 0 && height > 0)) return 1;
  return Math.max(0.01, Math.min(1, Math.max(roomWidth, 1) / width, Math.max(roomHeight, 1) / height));
}

// The zoom after a step of delta from the current one, where scale is what the current one draws at (Fit draws at fitScale): + is the
// next step larger than that, − the next smaller, 0 is Fit. At either end of the steps the zoom stays as it is.
export function stepPictureZoom(zoom, scale, delta) {
  if (delta === 0) return FIT;
  const near = 0.001;
  const next = delta > 0 ? PICTURE_ZOOMS.find(step => step > scale + near) : [...PICTURE_ZOOMS].reverse().find(step => step < scale - near);
  return next ?? zoom;
}

// The zoom as the student reads it: "37 %".
export const zoomLabel = scale => `${Math.round(scale * 100)} %`;

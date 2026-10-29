// Exam text arrives as HTML compiled from Markdown by shared/examdoc. The exam window has native bindings, so nothing in that
// HTML may run, load or navigate: only the elements and attributes the compiler writes survive, everything else is unwrapped
// to its text. Parsing happens in an inert <template>, where scripts never run and images never load.
//
// The one picture the compiler writes is <img src="data:image/…;base64,…" alt="…"> (shared/examimage). Its src is checked here
// the way the server checks it; an <img> that fails is removed whole, never left behind without its src. A picture embedded as
// an image cannot run scripts or load anything, which is why an SVG may be shown this way and never inlined.

const MAX_IMAGE_BYTES = 128 * 1024;
const MAX_IMAGE_BASE64 = 4 * Math.ceil(MAX_IMAGE_BYTES / 3);
const MAX_ALT_CHARACTERS = 500;
const imagePrefix = /^data:image\/(?:png|jpeg|gif|svg\+xml);base64,/;
const base64Payload = /^[A-Za-z0-9+/]+={0,2}$/;

function imageSource(src) {
  const prefix = imagePrefix.exec(src ?? '');
  if (!prefix) return false;
  const payload = src.slice(prefix[0].length);
  return payload.length <= MAX_IMAGE_BASE64 && base64Payload.test(payload);
}

const allowed = {
  P: [], BR: [], HR: [], H1: [], H2: [], H3: [], H4: [], H5: [], H6: [], BLOCKQUOTE: [],
  UL: [], OL: ['start'], LI: [], STRONG: [], EM: [], DEL: [], CODE: ['class'], PRE: [],
  TABLE: [], THEAD: [], TBODY: [], TR: [], TH: ['align'], TD: ['align'],
  SPAN: ['class', 'data-slot'], DIV: ['class', 'data-slot'],
  IMG: ['src', 'alt'],
};
// A value rule is a pattern or a function; an attribute that fails its rule is dropped.
const values = {
  class: /^(slot|language-[a-z0-9+#-]{1,32})$/,
  'data-slot': /^[A-Za-z0-9_.-]{1,128}$/,
  align: /^(left|center|right)$/,
  start: /^[0-9]{1,6}$/,
  src: imageSource,
  alt: value => [...value].length <= MAX_ALT_CHARACTERS,
};
const accepts = (name, value) => typeof values[name] === 'function' ? values[name](value) : values[name].test(value);

function clean(parent) {
  for (const node of [...parent.childNodes]) {
    if (node.nodeType === Node.TEXT_NODE) continue;
    if (node.nodeType !== Node.ELEMENT_NODE || !(node.tagName in allowed)) {
      // Comments go; an unknown element keeps only its (cleaned) children.
      if (node.nodeType === Node.ELEMENT_NODE) { clean(node.content ?? node); node.replaceWith(...(node.content ?? node).childNodes); }
      else node.remove();
      continue;
    }
    if (node.tagName === 'IMG' && !imageSource(node.getAttribute('src'))) { node.remove(); continue; }
    for (const { name, value } of [...node.attributes]) {
      if (!allowed[node.tagName].includes(name) || !accepts(name, value)) node.removeAttribute(name);
    }
    clean(node);
  }
}

/** Returns exam HTML reduced to the elements and attributes the exam compiler produces. */
export function sanitizeHTML(html) {
  const template = document.createElement('template');
  template.innerHTML = String(html ?? '');
  clean(template.content);
  return template.innerHTML;
}

/** Returns the answer slot placeholders inside a rendered element, in reading order. */
export function slotElements(root) {
  return [...root.querySelectorAll('[data-slot]')];
}

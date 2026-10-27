// Exam text arrives as HTML compiled from Markdown by shared/examdoc. The exam window has native bindings, so nothing in that
// HTML may run, load or navigate: only the elements and attributes the compiler writes survive, everything else is unwrapped
// to its text. Parsing happens in an inert <template>, where scripts never run and images never load.

const allowed = {
  P: [], BR: [], HR: [], H1: [], H2: [], H3: [], H4: [], H5: [], H6: [], BLOCKQUOTE: [],
  UL: [], OL: ['start'], LI: [], STRONG: [], EM: [], DEL: [], CODE: ['class'], PRE: [],
  TABLE: [], THEAD: [], TBODY: [], TR: [], TH: ['align'], TD: ['align'],
  SPAN: ['class', 'data-slot'], DIV: ['class', 'data-slot'],
};
const values = {
  class: /^(slot|language-[a-z0-9+#-]{1,32})$/,
  'data-slot': /^[A-Za-z0-9_.-]{1,128}$/,
  align: /^(left|center|right)$/,
  start: /^[0-9]{1,6}$/,
};

function clean(parent) {
  for (const node of [...parent.childNodes]) {
    if (node.nodeType === Node.TEXT_NODE) continue;
    if (node.nodeType !== Node.ELEMENT_NODE || !(node.tagName in allowed)) {
      // Comments go; an unknown element keeps only its (cleaned) children.
      if (node.nodeType === Node.ELEMENT_NODE) { clean(node.content ?? node); node.replaceWith(...(node.content ?? node).childNodes); }
      else node.remove();
      continue;
    }
    for (const { name, value } of [...node.attributes]) {
      if (!allowed[node.tagName].includes(name) || !values[name].test(value)) node.removeAttribute(name);
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

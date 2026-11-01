import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { relative, resolve, sep } from 'node:path';

const root = fileURLToPath(new URL('../../', import.meta.url));
const read = path => readFileSync(resolve(root, path), 'utf8');
const themePath = 'shared/theme/theme.css';
const theme = read(themePath);
// The first :root block is the palette and its derived roles.
const rootBlock = theme.match(/:root\s*\{([^}]+)\}/)[1];
const uncomment = text => text.replace(/\/\*[\s\S]*?\*\/|<!--[\s\S]*?-->/g, '');
const tokens = new Map([...uncomment(rootBlock).matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)].map(m => [m[1], m[2].trim()]));

export function composite([r, g, b, a = 1], background) {
  return [r, g, b].map((c, i) => c * a + background[i] * (1 - a)).concat(1);
}

// Resolve the actual theme declarations, not a second hardcoded copy of the palette.
export function color(value, trail = []) {
  if (value.startsWith('--')) {
    assert(!trail.includes(value), `Cyclic color token: ${[...trail, value].join(' -> ')}`);
    assert(tokens.has(value), `Unknown color token ${value}`);
    return color(tokens.get(value), [...trail, value]);
  }
  const variable = value.match(/^var\((--[\w-]+)\)$/);
  if (variable) return color(variable[1], trail);
  if (value === 'transparent') return [0, 0, 0, 0];
  if (/^#[\da-f]{6}$/i.test(value)) return [1, 3, 5].map(i => parseInt(value.slice(i, i + 2), 16)).concat(1);
  const rgb = value.match(/^rgba?\(([^)]+)\)$/);
  if (rgb) {
    const channels = rgb[1].split(/[\s,/]+/).filter(Boolean).map(Number);
    return channels.length === 3 ? [...channels, 1] : channels;
  }
  const mix = value.match(/^color-mix\(in srgb, (var\(--[\w-]+\)) (\d+)%, (var\(--[\w-]+\)|transparent)\)$/);
  if (mix) {
    const a = color(mix[1], trail), b = color(mix[3], trail), t = Number(mix[2]) / 100;
    const alpha = a[3] * t + b[3] * (1 - t);
    return [0, 1, 2].map(i => alpha ? (a[i] * a[3] * t + b[i] * b[3] * (1 - t)) / alpha : 0).concat(alpha);
  }
  assert.fail(`Unsupported color declaration: ${value}`);
}

export function contrast(foreground, background) {
  assert.equal(background[3], 1, 'Composite the background before computing contrast');
  const luminance = c => c.slice(0, 3).map(v => v / 255)
    .map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4)
    .reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0);
  const values = [luminance(composite(foreground, background)), luminance(background)].sort((a, b) => a - b);
  return (values[1] + .05) / (values[0] + .05);
}

// The sidebar is a gradient: every text and mark on it is checked against each of its stops.
const sideStops = [...tokens.get('--side-bg').matchAll(/#[\da-f]{6}/gi)].map(m => color(m[0]));

const proctor = name => `apps/proctor/frontend/src/${name}`;
const student = name => `apps/student/frontend/src/${name}`;
const styleOf = path => {
  const text = uncomment(read(path));
  return path.endsWith('.svelte') ? text.match(/<style>([\s\S]*?)<\/style>/)?.[1] ?? '' : text;
};
export function property(path, selector, name) {
  const values = [];
  for (const rule of styleOf(path).matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!rule[1].split(',').map(s => s.trim()).includes(selector)) continue;
    for (const declaration of rule[2].matchAll(/([\w-]+)\s*:\s*([^;]+);/g)) {
      if (declaration[1] === name) values.push(declaration[2].trim());
    }
  }
  assert(values.length, `${path}: missing ${selector} { ${name} }`);
  return values.at(-1);
}

function files(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = resolve(directory, entry.name);
    return entry.isDirectory() ? files(path) : [path];
  });
}
const themed = () => [proctor(''), student(''), 'shared/theme'].flatMap(directory => files(resolve(root, directory)))
  .map(path => relative(root, path).split(sep).join('/'));

// All CSS named colors, not just the former blue palette. Paint keywords such as
// currentColor/inherit/none remain contextual; transparent must use its theme token.
const namedColors = new Set(('aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato transparent turquoise violet wheat white whitesmoke yellow yellowgreen').split(' '));

export function rawColors(source) {
  const text = uncomment(source);
  const found = [...text.matchAll(/#[\da-f]{3,8}\b|\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\s*\(/gi)].map(m => m[0]);
  // CSS declarations, SVG presentation attributes, and CodeMirror's JS style objects.
  for (const m of text.matchAll(/\b(?:[\w-]*(?:color|background|border|shadow|outline)[\w-]*|fill|stroke)\s*[:=]\s*([^;\n}]+)/gi)) {
    const value = m[1].replace(/var\(--[\w-]+(?:,\s*([^)]*))?\)/g, '$1').replace(/["']/g, '');
    for (const word of value.matchAll(/\b[a-z]+\b/gi)) if (namedColors.has(word[0].toLowerCase())) found.push(word[0]);
  }
  return found;
}

// Selectors checked per area; each commit of the theme adds its own.
const selectorChecks = [
  [themePath, 'body', 'background', 'var(--canvas-wash) fixed, var(--bg-primary)'],
  [themePath, 'button', 'background', 'var(--bg-secondary)'],
  [themePath, 'button:hover:not(:disabled)', 'background', 'var(--selection-fill)'],
  [themePath, 'button.primary', 'background', 'var(--action-primary)'],
  [themePath, 'button.primary', 'color', 'var(--bg-primary)'],
  [themePath, 'button.primary:hover:not(:disabled)', 'background', 'var(--action-hover)'],
  [themePath, 'button:disabled', 'color', 'var(--muted-dot)'],
  [themePath, 'button:disabled', 'box-shadow', 'none'],
  [themePath, 'input:not([type="checkbox"]):not([type="radio"]):focus-visible', 'border-color', 'var(--action-primary)'],
  [themePath, 'input:not([type="checkbox"]):not([type="radio"]):focus-visible', 'box-shadow', '0 0 0 3px var(--focus-halo)'],
  [themePath, ':focus-visible', 'outline', '2px solid var(--action-primary)'],
  [themePath, '::selection', 'background', 'var(--selection-fill)'],
  [themePath, '.eyebrow', 'color', 'var(--action-primary)'],
  [themePath, '.callout.warning', '--callout-text', 'var(--warn-text)'],
  ['shared/theme/Card.svelte', '.card', 'background', 'var(--bg-raised)'],
  ['shared/theme/Card.svelte', '.deep', '--band', 'var(--header-strong)'],
  ['shared/theme/Card.svelte', '.card-head', 'color', 'var(--band-ink)'],
  ['shared/theme/calculator/Calculator.svelte', '.calc-actions .equals', 'background', 'var(--action-primary)'],
  ['shared/theme/calculator/Programmer.svelte', '.key.equals', 'background', 'var(--action-primary)'],
  ['shared/theme/calculator/Programmer.svelte', '.nibble .one', 'background', 'var(--action-hover)'],
  ['shared/theme/calculator/Programmer.svelte', '.key.fn', 'background', 'var(--hover-fill)'],
  ['shared/theme/calculator/Programmer.svelte', '.key.sym', 'background', 'var(--transparent)'],
];

export function registerThemeTests() {
  test('contrast calculation uses WCAG luminance and alpha compositing', () => {
    assert.equal(contrast([0, 0, 0, 1], [255, 255, 255, 1]), 21);
    assert.equal(contrast([0, 0, 0, 0], [255, 255, 255, 1]), 1);
    assert.deepEqual(composite([0, 0, 0, .5], [255, 255, 255, 1]), [127.5, 127.5, 127.5, 1]);
    assert.throws(() => color('--not-a-token'));
  });

  test('the palette is the mockups’ own, verbatim', () => {
    const palette = {
      '--bg-primary': '#fdf6e3', '--bg-secondary': '#eee8d5', '--bg-raised': '#fffbf2', '--border-subtle': '#ddd6c1',
      '--text-main': '#0a2342', '--text-muted': '#586e75', '--ocean-deep': '#03045e', '--action-primary': '#0077b6',
      '--action-hover': '#023e8a', '--accent-cyan': '#00b4d8', '--accent-cyan-soft': '#48cae4', '--selection-fill': '#caf0f8',
      '--hover-fill': '#e3f6fa', '--warn': '#b58900', '--warn-fill': '#f5e9c4', '--warn-text': '#6b5200', '--alert': '#cb4b16',
      '--alert-fill': '#fbefe6', '--ok': '#859900', '--ok-fill': '#f6f6dc', '--ok-text': '#4f5c00', '--success-badge': '#1d7a73',
      '--side-text': '#d8e4ef', '--side-text-dim': '#8fa6bd',
    };
    for (const [name, value] of Object.entries(palette)) assert.equal(tokens.get(name), value, name);
    // Every other token is either one of the mockups' two remaining literals or derived from the palette.
    const literals = [...tokens].filter(([name, value]) => /#[\da-f]{3,8}\b/i.test(value) && !(name in palette));
    assert.deepEqual(literals.map(([name]) => name).sort(), ['--muted-dot', '--paper', '--side-bg']);
  });

  test('every text pair the apps paint reaches 4.5:1', () => {
    const pairs = [
      // Body text and headings on every ground; muted text where it is used (never on --bg-secondary, --selection-fill
      // or --warn-fill, where it falls under 4.5:1).
      ...['bg-primary', 'bg-raised', 'bg-secondary', 'selection-fill', 'hover-fill', 'warn-fill', 'ok-fill', 'alert-fill'].map(bg => ['text-main', bg]),
      ...['bg-primary', 'bg-raised', 'hover-fill', 'ok-fill', 'alert-fill'].map(bg => ['text-muted', bg]),
      ['action-primary', 'bg-primary'], ['action-primary', 'bg-raised'], // eyebrows, links, the toggle hint
      ...['bg-raised', 'bg-secondary', 'selection-fill', 'hover-fill'].map(bg => ['action-hover', bg]), // soft headers, pills, ghost hover
      ['bg-primary', 'action-primary'], ['bg-primary', 'action-hover'], ['bg-primary', 'success-badge'], // primary buttons, Validated
      ['warn-text', 'warn-fill'], ['warn-text', 'bg-raised'], ['ok-text', 'ok-fill'], ['ok-text', 'bg-raised'],
      ['alert-text', 'alert-fill'], ['alert-text', 'bg-raised'], ['alert-text', 'bg-primary'], ['alert-text', 'bg-secondary'],
      ['on-dark', 'ocean-deep'], ['on-dark', 'action-hover'], ['accent-cyan-soft', 'ocean-deep'], ['accent-cyan-soft', 'action-hover'],
    ];
    for (const [fg, bg] of pairs) {
      const ratio = contrast(color(`--${fg}`), color(`--${bg}`));
      assert(ratio >= 4.5, `${fg} on ${bg}: ${ratio.toFixed(3)}:1 < 4.5`);
    }
    // The dim sidebar text (inactive icons, the server address) never sits on the active item, which turns white.
    for (const stop of sideStops) {
      const grounds = { plain: stop, hover: composite(color('--side-hover'), stop), active: composite(color('--side-active'), stop) };
      for (const [fg, on] of [['side-text', ['plain', 'hover', 'active']], ['on-dark', ['plain', 'hover', 'active']],
        ['accent-cyan-soft', ['plain', 'hover', 'active']], ['side-text-dim', ['plain', 'hover']]]) {
        for (const ground of on) {
          const ratio = contrast(color(`--${fg}`), grounds[ground]);
          assert(ratio >= 4.5, `${fg} on the sidebar (${ground}): ${ratio.toFixed(3)}:1 < 4.5`);
        }
      }
    }
  });

  test('focus rings, control edges, status edges and dots reach 3:1', () => {
    const pairs = [
      ...['bg-primary', 'bg-raised', 'bg-secondary'].map(bg => ['action-primary', bg]), // focus outline, spinner on a disabled button
      ['control-border', 'bg-primary'], ['control-border', 'bg-raised'], // form-control edges (WCAG 1.4.11)
      ...['warn', 'alert', 'ok'].map(fg => [fg, 'bg-raised']), // callout and card edges, row bars
    ];
    for (const [fg, bg] of pairs) {
      const ratio = contrast(color(`--${fg}`), color(`--${bg}`));
      assert(ratio >= 3, `${fg} on ${bg}: ${ratio.toFixed(3)}:1 < 3`);
    }
    for (const stop of sideStops) {
      for (const fg of ['accent-cyan-soft', 'ok', 'warn', 'alert', 'muted-dot', 'accent-cyan']) {
        const ratio = contrast(color(`--${fg}`), stop);
        assert(ratio >= 3, `${fg} on the sidebar: ${ratio.toFixed(3)}:1 < 3`);
      }
    }
  });

  test('the component selectors actually use the approved roles', () => {
    for (const [path, selector, name, expected] of selectorChecks) assert.equal(property(path, selector, name), expected, `${path}: ${selector} ${name}`);
  });

  test('--ok, --warn and --alert are never a background behind text, only behind dots and edges', () => {
    for (const path of themed().filter(name => /\.(css|svelte)$/.test(name))) {
      for (const rule of styleOf(path).matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
        if (!/\bbackground(?:-color)?\s*:\s*var\(--(?:ok|warn|alert)\)/.test(rule[2])) continue;
        for (const selector of rule[1].split(',').map(s => s.trim())) {
          assert(/dot|::?before|::?after|bar\b/.test(selector), `${path}: solid status color behind ${selector}`);
        }
      }
    }
  });

  test('both apps and shared components define no raw colors outside theme.css', () => {
    const failures = [];
    const logo = student('assets/cbe-portal-logo.svg'); // sole owner-approved literal exception
    // The logo palette is frozen (owner decision): token value changes do not recolor it.
    const allowedLogoPaints = new Set(['#FFFFFF', '#F6F3EC', '#DFF0EB', '#1E5E58', '#F1E7D2', '#56656E', '#DCD5C8']);
    for (const name of themed()) {
      if (!/\.(?:css|svelte|js|mjs|svg)$/.test(name) || name === themePath) continue;
      const text = read(name);
      if (name === logo) {
        for (const paint of rawColors(text)) assert(allowedLogoPaints.has(paint.toUpperCase()), `Unapproved logo paint ${paint}`);
        continue;
      }
      const raw = rawColors(text);
      if (raw.length) failures.push(`${name}: ${raw.join(', ')}`);
    }
    assert.deepEqual(failures, []);
    assert(!/--[\w-]+\s*:/.test(uncomment(read(proctor('dashboard.css'))).replace(/--radius-[\w-]+\s*:/g, '')), 'Dashboard must not own color tokens');
  });

  test('every token a component names is defined by the theme or by the component itself', () => {
    // The theme defines the palette; the proctor's dashboard.css adds only its two radii.
    const defined = new Set([...uncomment(theme + read(proctor('dashboard.css'))).matchAll(/(--[\w-]+)\s*:/g)].map(m => m[1]));
    const failures = [];
    for (const name of themed().filter(path => /\.(?:css|svelte|js)$/.test(path) && path !== themePath)) {
      const text = uncomment(read(name));
      const local = new Set([...text.matchAll(/(--[\w-]+)\s*[:=]/g)].map(m => m[1]));
      for (const m of text.matchAll(/var\((--[\w-]+)\s*(,)?/g)) {
        if (!m[2] && !defined.has(m[1]) && !local.has(m[1])) failures.push(`${name}: ${m[1]}`);
      }
    }
    assert.deepEqual([...new Set(failures)], []);
  });

  test('the raw-color guard catches fallback, functional, named and SVG paints', () => {
    for (const source of ['color: #abc;', 'color: var(--danger, #b3261e);', 'fill="rgb(1 2 3)"', "backgroundColor: 'hsl(0 0% 0%)'", 'border: 1px solid rebeccapurple;', 'background: transparent;', 'color: var(--ink, red);', "backgroundImage: 'linear-gradient(red, blue)'"]) assert(rawColors(source).length, source);
    assert.deepEqual(rawColors('color: var(--text-main); stroke="currentColor"; fill="none";'), []);
  });

  test('Lucide action icons are local, inherit color and include the approved locked door', () => {
    for (const name of ['monitoring-exam', 'preview-exam', 'publish-exam', 'close-admission', 'download-submissions', 'purge-screenshots', 'delete-exam', 'tools']) {
      const svg = read(proctor(`icons/${name}.svg`));
      assert(svg.includes('viewBox="0 0 24 24"'));
      assert(svg.includes('stroke="currentColor"'));
      assert(!/<(?:script|image|foreignObject)\b|\bon\w+=|href=/.test(svg), `Non-static SVG ${name}`);
    }
    const door = read(proctor('icons/close-admission.svg'));
    assert(door.includes('<rect width="8" height="5" x="14" y="16" rx="1"/>'), 'Lucide door-closed-locked lock shape');
    assert(read('apps/proctor/frontend/public/lucide-LICENSE.txt').includes('Copyright (c) 2013-present Cole Bemis'));
    assert(read(proctor('Icon.svelte')).includes("'./icons/close-admission.svg?raw'"));
  });
}

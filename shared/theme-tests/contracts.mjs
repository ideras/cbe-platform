import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { relative, resolve } from 'node:path';

const root = fileURLToPath(new URL('../../', import.meta.url));
const read = path => readFileSync(resolve(root, path), 'utf8');
const themePath = 'shared/theme/warm.css';
const theme = read(themePath);
const rootBlock = theme.match(/:root\s*\{([^}]+)\}/)[1];
const tokens = new Map([...rootBlock.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)].map(m => [m[1], m[2].trim()]));
const uncomment = text => text.replace(/\/\*[\s\S]*?\*\/|<!--[\s\S]*?-->/g, '');

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

const proctor = name => `apps/proctor/frontend/src/${name}`;
const student = name => `apps/student/frontend/src/${name}`;
function property(path, selector, name) {
  const text = uncomment(read(path));
  const source = path.endsWith('.svelte') ? text.match(/<style>([\s\S]*?)<\/style>/)[1] : text;
  const values = [];
  for (const rule of source.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
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

export function registerThemeTests() {
  test('contrast calculation uses WCAG luminance and alpha compositing', () => {
    assert.equal(contrast([0, 0, 0, 1], [255, 255, 255, 1]), 21);
    assert.equal(contrast([0, 0, 0, 0], [255, 255, 255, 1]), 1);
    assert.deepEqual(composite([0, 0, 0, .5], [255, 255, 255, 1]), [127.5, 127.5, 127.5, 1]);
    assert.throws(() => color('--not-a-token'));
  });

  test('all section-4 and student/calculator text pairs reach 4.5:1', () => {
    const pairs = [
      // Page/title, exam titles, ids, headers, neutral banners, control text, selections.
      ...['page', 'surface', 'hover', 'sea', 'sand', 'coral-tint'].flatMap(bg => [['ink', bg], ['ink-2', bg]]),
      ['sea-ink', 'sea'], ['sea-ink', 'surface'], // success, Published, calculator feature
      ['sand-ink', 'sand'], ['sand-ink', 'surface'], // Monitoring, Forward only, pending/network
      ['coral', 'surface'], ['coral', 'coral-tint'], // danger normal/hover; focus-loss badges
      ['surface', 'ocean'], ['surface', 'ocean-strong'], // primary, calculator mode/equals
      ['surface', 'ink'], // exercise pills, programmer segments and set bits
      ['sidebar-ink', 'sidebar'], ['sidebar-ink', 'sidebar-raised'], ['sidebar-ink', 'sidebar-status'],
      ['sidebar-ink-2', 'sidebar'],
    ];
    for (const [fg, bg] of pairs) {
      const ratio = contrast(color(`--${fg}`), color(`--${bg}`));
      assert(ratio >= 4.5, `${fg} on ${bg}: ${ratio.toFixed(3)}:1 < 4.5`);
    }
  });

  test('section-4 control borders, icons, focus and labelled dots reach 3:1', () => {
    const pairs = [
      ...['surface', 'hover', 'page', 'sea', 'sand', 'coral-tint'].flatMap(bg => [['ink-2', bg], ['ocean', bg]]),
      ['sea-ink', 'sea'], ['sand-ink', 'sand'], ['coral', 'surface'], ['coral', 'coral-tint'],
      ['ocean', 'page'], ['ocean-strong', 'surface'],
      ...['sidebar', 'sidebar-raised', 'sidebar-status'].flatMap(bg => [['sidebar-ink', bg], ['sidebar-ink-2', bg]]),
      ['ok', 'surface'], ['ok', 'sea'],
      ...['sidebar', 'sidebar-raised', 'sidebar-status'].flatMap(bg => [['ok-on-dark', bg], ['sand', bg], ['coral-tint', bg]]),
    ];
    for (const [fg, bg] of pairs) {
      const ratio = contrast(color(`--${fg}`), color(`--${bg}`));
      assert(ratio >= 3, `${fg} on ${bg}: ${ratio.toFixed(3)}:1 < 3`);
    }
    // A dot is adjacent to its halo too, not just the panel behind the halo.
    for (const bg of ['sidebar', 'sidebar-raised', 'sidebar-status']) {
      const adjacent = composite(color('--sidebar-dot-halo'), color(`--${bg}`));
      assert(contrast(color('--ok-on-dark'), adjacent) >= 3, `connected dot against halo on ${bg}`);
    }
    assert(contrast(color('--ok'), composite(color('--status-dot-halo'), color('--sea'))) >= 3, 'admitting dot against halo');
    const pulse = Number(read(proctor('Sidebar.svelte')).match(/@keyframes pulse\s*\{\s*50%\s*\{\s*opacity:\s*([\d.]+)/)[1]);
    for (const bg of ['sidebar', 'sidebar-raised', 'sidebar-status']) {
      const dot = color('--sidebar-ink'); dot[3] = pulse;
      assert(contrast(dot, color(`--${bg}`)) >= 3, `connecting pulse minimum on ${bg}`);
    }
    // Connecting disables the primary button, so the spinner is on a muted surface.
    const spinner = property(proctor('ConnectionDialog.svelte'), '.spinner', 'border-top-color');
    assert(contrast(color(spinner), color('--surface')) >= 3, 'connecting spinner on disabled button');
  });

  test('the component selectors actually use the approved semantic roles', () => {
    const checks = [
      [themePath, 'button', 'color', 'var(--ink)'],
      [themePath, 'button', 'border', '1px solid var(--ink-2)'],
      [themePath, 'button.primary', 'background', 'var(--ocean)'],
      [themePath, 'button.primary:hover:not(:disabled)', 'background', 'var(--ocean-strong)'],
      [themePath, '::selection', 'background', 'var(--selected)'],
      [proctor('Sidebar.svelte'), '.sidebar', 'background', 'var(--sidebar)'],
      [proctor('Sidebar.svelte'), '.item:focus-visible', 'outline', '3px solid var(--nav-accent)'],
      [proctor('Sidebar.svelte'), '.dot.connected', 'background', 'var(--ok-on-dark)'],
      [proctor('Sidebar.svelte'), '.dot.connecting', 'background', 'var(--sidebar-ink)'],
      [proctor('Sidebar.svelte'), '.dot.failed', 'background', 'var(--coral-tint)'],
      [proctor('AdmittingBar.svelte'), '.admitting.closed', 'background', 'var(--surface)'],
      [proctor('AdmittingBar.svelte'), '.closed .dot', 'background', 'var(--ink-2)'],
      [proctor('ClassDetail.svelte'), '.tabs button[aria-selected="true"]', 'color', 'var(--ink)'],
      [proctor('ClassDetail.svelte'), '.tabs button[aria-selected="true"]', 'border-bottom-color', 'var(--ocean)'],
      [proctor('ClassExams.svelte'), 'th', 'color', 'var(--ink-2)'],
      [proctor('ClassExams.svelte'), 'td', 'border-bottom', '1px solid var(--line)'],
      [proctor('ClassExams.svelte'), '.calc', 'background', 'var(--sea)'],
      [proctor('ClassExams.svelte'), '.calc', 'color', 'var(--sea-ink)'],
      [proctor('ClassExams.svelte'), '.calc', 'border-style', 'dashed'],
      [proctor('ClassExams.svelte'), '.watching', 'background', 'var(--transparent)'],
      [proctor('Monitoring.svelte'), '.pending', 'background', 'var(--transparent)'],
      [student('CodeEditor.svelte'), '.editor:focus-within', 'border-color', 'var(--ocean)'],
      ['shared/theme/Card.svelte', '.card-head', 'color', 'var(--band-ink)'],
      ['shared/theme/calculator/Calculator.svelte', '.calc-actions .equals', 'background', 'var(--ocean)'],
      ['shared/theme/calculator/Programmer.svelte', '.key.equals', 'background', 'var(--ocean)'],
      ['shared/theme/calculator/Programmer.svelte', '.nibble .one', 'background', 'var(--ink)'],
      ['shared/theme/calculator/Programmer.svelte', '.key.fn', 'background', 'var(--sea)'],
      ['shared/theme/calculator/Programmer.svelte', '.key.sym', 'background', 'var(--transparent)'],
    ];
    for (const [path, selector, name, expected] of checks) assert.equal(property(path, selector, name), expected, `${path}: ${selector} ${name}`);
    assert.equal(tokens.get('--nav-accent'), 'var(--sidebar-ink)');
    assert.equal(tokens.get('--color-focus'), 'var(--coral)');
    assert.equal(tokens.get('--color-success'), 'var(--sea-ink)');
    assert.equal(tokens.get('--color-control-border'), 'var(--ink-2)');
    assert.notDeepEqual(color('--color-focus-bg'), color('--color-warning-bg'));
  });

  test('both apps and shared components define no raw colors outside warm.css', () => {
    const failures = [];
    const logo = student('assets/cbe-portal-logo.svg'); // sole owner-approved literal exception
    // The logo palette is frozen (owner decision): token value changes do not recolor it.
    const allowedLogoPaints = new Set(['#FFFFFF', '#F6F3EC', '#DFF0EB', '#1E5E58', '#F1E7D2', '#56656E', '#DCD5C8']);
    for (const directory of [proctor(''), student(''), 'shared/theme']) {
      for (const path of files(resolve(root, directory))) {
        const name = relative(root, path);
        if (!/\.(?:css|svelte|js|mjs|svg)$/.test(name) || name === themePath) continue;
        const text = readFileSync(path, 'utf8');
        if (name === logo) {
          for (const paint of rawColors(text)) assert(allowedLogoPaints.has(paint.toUpperCase()), `Unapproved logo paint ${paint}`);
          continue;
        }
        const raw = rawColors(text);
        if (raw.length) failures.push(`${name}: ${raw.join(', ')}`);
        assert(!/--ocean-\d/.test(text), `${name}: obsolete numeric ocean token`);
      }
    }
    assert.deepEqual(failures, []);
    assert(!/--(?:brand|nav|page-bg|panel|title-ink)[\w-]*\s*:/.test(read(proctor('dashboard.css'))), 'Dashboard must not own color tokens');
  });

  test('ocean is used only by the approved primary, tab, focus and calculator selectors', () => {
    for (const directory of [proctor(''), student(''), 'shared/theme']) {
      for (const path of files(resolve(root, directory))) {
        if (!/\.(css|svelte)$/.test(path)) continue;
        const text = uncomment(readFileSync(path, 'utf8'));
        const css = path.endsWith('.svelte') ? text.match(/<style>([\s\S]*?)<\/style>/)?.[1] || '' : text;
        for (const rule of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
          if (!/var\(--ocean(?:-strong)?\)/.test(rule[2])) continue;
          for (const selector of rule[1].split(',').map(s => s.trim())) {
            const approved = selector === ':root' || /\.primary\b|:focus-visible|:focus-within/.test(selector)
              || /\.calc-tabs button\[aria-selected|\.calc-actions \.equals|\.key\.equals/.test(selector)
              || selector === '.tabs button[aria-selected="true"]';
            assert(approved, `${relative(root, path)}: ocean on ${selector}`);
          }
        }
      }
    }
  });

  test('the raw-color guard catches fallback, functional, named and SVG paints', () => {
    for (const source of ['color: #abc;', 'color: var(--danger, #b3261e);', 'fill="rgb(1 2 3)"', "backgroundColor: 'hsl(0 0% 0%)'", 'border: 1px solid rebeccapurple;', 'background: transparent;', 'color: var(--ink, red);', "backgroundImage: 'linear-gradient(red, blue)'"]) assert(rawColors(source).length, source);
    assert.deepEqual(rawColors('color: var(--ink); stroke="currentColor"; fill="none";'), []);
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

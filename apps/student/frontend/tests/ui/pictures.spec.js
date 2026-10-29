import { test, expect } from '@playwright/test';

// What the exam compiler writes for a picture: <img src="data:image/…;base64,…" alt="…"> and nothing else.
const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
const svg = Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="#000"/></svg>').toString('base64');
const wide = Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="3000" height="300"><rect width="3000" height="300" fill="#036"/></svg>').toString('base64');
const picture = (type, data, alt) => `<img src="data:image/${type};base64,${data}" alt="${alt}">`;

async function login(page) {
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}

// Opens an exam with one exercise whose body is `body`, with a text answer q1 and a blank q2 in a table cell.
async function open(page, body, extra = {}) {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.exam(value), {
    navigation: 'free',
    content: { instructions: extra.instructions ?? '', exercises: [{ id: '1', title: 'Figuras', body, slots: [{ id: 'q1', kind: 'text', lines: 3 }, { id: 'q2', kind: 'blank' }] }] },
  });
  await login(page);
}

const table = `<table><thead><tr><th>Figura</th><th>Respuesta</th></tr></thead><tbody><tr><td>${picture('png', png, 'Un punto')}</td><td><span class="slot" data-slot="q2"></span></td></tr></tbody></table>`;

test('a picture compiled into the exam is shown, and answer fields next to it still work', async ({ page }) => {
  const external = [];
  await page.route('**/*', route => { if (!/^(http:\/\/127\.0\.0\.1:4178\/|data:)/.test(route.request().url())) { external.push(route.request().url()); return route.abort(); } return route.continue(); });
  await open(page, `<p>Antes</p>${picture('svg+xml', svg, 'Autómata del Lexer: estados S0 a S11')}<div class="slot" data-slot="q1"></div>${table}`);
  const figure = page.getByRole('img', { name: 'Autómata del Lexer: estados S0 a S11' });
  await expect(figure).toBeVisible();
  expect(await figure.evaluate(image => image.complete && image.naturalWidth)).toBe(40);
  await expect(page.getByRole('img', { name: 'Un punto' })).toBeVisible();
  // A white background keeps black drawings readable on the dark theme.
  await expect(figure).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  // The field right after the picture, and the blank in the cell next to another picture, take answers.
  const text = page.getByRole('textbox', { name: 'Answer for q1' });
  await text.fill('respuesta junto a la figura');
  await expect(text).toHaveValue('respuesta junto a la figura');
  const blank = page.getByRole('textbox', { name: 'Answer for q2' });
  await expect(page.getByRole('cell').filter({ has: blank })).toBeVisible();
  await blank.fill('42');
  await expect(blank).toHaveValue('42');
  await expect(page.getByRole('cell').filter({ has: page.getByRole('img', { name: 'Un punto' }) })).toBeVisible();
  expect(external).toEqual([]);
});

test('a picture in the general instructions is shown, and one wider than the window shrinks to fit', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 700 });
  await open(page, '<div class="slot" data-slot="q1"></div>', { instructions: `<p>Lea la figura.</p>${picture('svg+xml', wide, 'Un diagrama muy ancho')}` });
  const figure = page.getByRole('img', { name: 'Un diagrama muy ancho' });
  await expect(figure).toBeVisible();
  const [box, container] = await Promise.all([figure.boundingBox(), figure.evaluate(image => image.parentElement.getBoundingClientRect().width)]);
  expect(box.width).toBeLessThanOrEqual(container + 0.5);
  expect(box.width).toBeGreaterThan(100);
  expect(Math.round(box.width / box.height)).toBe(10);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('an <img> that is not exactly an embedded picture is removed, never left half there', async ({ page }) => {
  const external = []; const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.route('**/*', route => { if (!/^(http:\/\/127\.0\.0\.1:4178\/|data:)/.test(route.request().url())) { external.push(route.request().url()); return route.abort(); } return route.continue(); });
  const bad = [
    '<img src="http://example.invalid/a.png" alt="remote">',
    '<img src="https://example.invalid/a.png" alt="remote">',
    '<img src="//example.invalid/a.png" alt="remote">',
    '<img src="javascript:window.__unsafe=true" alt="script">',
    '<img src="x" alt="relative" onerror="window.__unsafe=true">',
    '<img alt="no source">',
    '<img src="data:text/html;base64,PHNjcmlwdD53aW5kb3cuX191bnNhZmU9dHJ1ZTwvc2NyaXB0Pg==" alt="html">',
    '<img src="data:image/webp;base64,AAAA" alt="webp">',
    '<img src="data:image/png;base64,AAA A" alt="space">',
    '<img src="data:image/png,not-base64" alt="not base64">',
    `<img src="data:image/png;base64,${'A'.repeat(4 * Math.ceil(128 * 1024 / 3) + 4)}" alt="too long">`,
    '<img src="data:image/svg+xml;utf8,<svg xmlns=\'http://www.w3.org/2000/svg\' onload=\'window.__unsafe=true\'/>" alt="inline svg">',
  ].join('');
  await open(page, `${bad}${picture('png', png, 'La buena')}<div class="slot" data-slot="q1"></div>`);
  await expect(page.getByRole('img', { name: 'La buena' })).toBeVisible();
  await expect(page.locator('main img')).toHaveCount(1);
  expect(await page.evaluate(() => window.__unsafe)).toBeUndefined();
  expect(external).toEqual([]); expect(errors).toEqual([]);
});

test('a picture keeps only its source and alternative text', async ({ page }) => {
  const noisy = picture('png', png, 'Con ruido').replace('<img ', '<img srcset="http://example.invalid/b.png 2x" style="position:fixed;width:9999px" width="9999" height="9999" class="slot" loading="eager" id="x" onclick="window.__unsafe=true" data-slot="q9" ');
  await open(page, `${noisy}<div class="slot" data-slot="q1"></div>`);
  const figure = page.getByRole('img', { name: 'Con ruido' });
  await expect(figure).toBeVisible();
  expect(await figure.evaluate(image => image.getAttributeNames().sort())).toEqual(['alt', 'src']);
  expect(await page.evaluate(() => window.__unsafe)).toBeUndefined();
});

test('an alternative text longer than 500 characters is dropped, a shorter one kept as text only', async ({ page }) => {
  await open(page, `${picture('png', png, 'x'.repeat(501))}${picture('png', png, 'Corta &amp; &lt;clara&gt;')}<div class="slot" data-slot="q1"></div>`);
  await expect(page.locator('main img')).toHaveCount(2);
  const alts = await page.locator('main img').evaluateAll(images => images.map(image => image.getAttribute('alt')));
  expect(alts).toEqual([null, 'Corta & <clara>']);
});

// A second line of defense behind the sanitizer: the page itself refuses to load an image from anywhere but its own origin or a
// data: URI, so a remote <img> that somehow got in would not be fetched.
test('the page policy refuses a remote image even if one is inserted', async ({ page }) => {
  const requests = [];
  await page.route('**/*', route => { if (!/^(http:\/\/127\.0\.0\.1:4178\/|data:)/.test(route.request().url())) { requests.push(route.request().url()); return route.abort(); } return route.continue(); });
  await page.goto('/');
  const violation = await page.evaluate(() => new Promise(resolve => {
    document.addEventListener('securitypolicyviolation', event => resolve({ directive: event.effectiveDirective, blocked: event.blockedURI }), { once: true });
    const image = document.createElement('img');
    image.src = 'https://example.invalid/tracker.png';
    document.body.append(image);
  }));
  expect(violation).toEqual({ directive: 'img-src', blocked: 'https://example.invalid/tracker.png' });
  expect(requests).toEqual([]);
  // The application's own logo, served from its origin, still loads.
  await expect.poll(() => page.evaluate(() => [...document.images].filter(image => image.alt.includes('logo')).every(image => image.complete && image.naturalWidth > 0))).toBe(true);
});

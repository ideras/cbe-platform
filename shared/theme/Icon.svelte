<script>
  // The icons of both applications: trusted SVGs bundled from ./icons at build time. No runtime Iconify API, URL or
  // user-supplied markup. Tabler icons, plus Fluent's "desktop pulse" for monitoring and Lucide's locked door for closing
  // admission; sources and licenses are in each application's public/icons-LICENSE.txt.
  // An icon is named after its file, in camelCase: close-admission.svg is "closeAdmission". Each SVG sets its own fill and
  // stroke from currentColor, so an icon takes the text color of wherever it sits.
  const files = import.meta.glob('./icons/*.svg', { query: '?raw', import: 'default', eager: true });
  const shapes = Object.fromEntries(Object.entries(files).map(([path, svg]) => [
    path.slice('./icons/'.length, -'.svg'.length).replace(/-(\w)/g, (_, letter) => letter.toUpperCase()),
    svg.replace(/^\s*<svg[^>]*>|<\/svg>\s*$/g, ''),
  ]));

  let { name, size = 24, ...rest } = $props();
</script>
<svg viewBox="0 0 24 24" width={size} height={size} aria-hidden="true" focusable="false" {...rest}>{@html shapes[name]}</svg>

<script>
  // Trusted, bundled SVGs only: no runtime Iconify API, URL or user-supplied markup.
  // Lucide/Feather license notices are in public/lucide-LICENSE.txt (also shipped in the build).
  import monitor from './icons/monitoring-exam.svg?raw';
  import preview from './icons/preview-exam.svg?raw';
  import publish from './icons/publish-exam.svg?raw';
  import closeAdmission from './icons/close-admission.svg?raw';
  import download from './icons/download-submissions.svg?raw';
  import purge from './icons/purge-screenshots.svg?raw';
  import deleteExam from './icons/delete-exam.svg?raw';
  import calculator from './icons/tools.svg?raw';

  let { name, size = 24, ...rest } = $props();
  const actions = Object.fromEntries(Object.entries({ monitor, preview, publish, closeAdmission, download, purge, deleteExam, calculator })
    .map(([key, svg]) => [key, svg.replace(/^<svg[^>]*>|<\/svg>\s*$/g, '')]));
  const shapes = {
    ...actions,
    server: '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5"/><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
    classes: '<circle cx="12" cy="7.5" r="3"/><path d="M6.5 20v-1.2A4.3 4.3 0 0 1 10.8 14h2.4a4.3 4.3 0 0 1 4.3 4.8V20"/><circle cx="4.8" cy="9.5" r="2.1"/><path d="M1.6 17.6v-.4a3.2 3.2 0 0 1 3.2-3.2"/><circle cx="19.2" cy="9.5" r="2.1"/><path d="M22.4 17.6v-.4a3.2 3.2 0 0 0-3.2-3.2"/>',
    exams: '<path d="M14 2H7a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7z"/><path d="M14 2v5h5"/><path d="M9 12h6"/><path d="M9 16h6"/><path d="M9 8h2"/>',
    power: '<path d="M12 3v8"/><path d="M17.7 6.3a8 8 0 1 1-11.4 0"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.6-3.6"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    chevron: '<path d="m9 6 6 6-6 6"/>',
    back: '<path d="m11 6-6 6 6 6"/><path d="M5 12h14"/>',
    close: '<path d="M6 6l12 12"/><path d="M18 6 6 18"/>',
    folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2.5h8a2 2 0 0 1 2 2V18a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    refresh: '<path d="M20 11a8 8 0 0 0-14.5-4.5L4 8"/><path d="M4 4v4h4"/><path d="M4 13a8 8 0 0 0 14.5 4.5L20 16"/><path d="M20 20v-4h-4"/>',
    lock: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
    gear: '<circle cx="12" cy="12" r="3.1"/><path d="M12 2.6v2.6M12 18.8v2.6M21.4 12h-2.6M5.2 12H2.6M18.6 5.4l-1.8 1.8M7.2 16.8l-1.8 1.8M18.6 18.6l-1.8-1.8M7.2 7.2 5.4 5.4"/>',
  };
</script>
<svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false" {...rest}>{@html shapes[name]}</svg>

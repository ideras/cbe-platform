import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

const resolve = path => fileURLToPath(new URL(path, import.meta.url));

export default defineConfig(({ command, mode }) => {
  if (command === 'build' && mode !== 'production') throw new Error('Only production mode may build shipping assets.');
  return {
    plugins: [svelte()],
    resolve: { alias: { '$student-api': resolve(mode === 'ui-test' ? './tests/fake-api.js' : './src/api.js') } },
    base: './',
    server: { fs: { allow: [resolve('.'), resolve('../../../shared/theme'), resolve('../../../shared/exam')] } },
    build: {
      rolldownOptions: {
        external: ['/wails/runtime.js'],
        output: { codeSplitting: { groups: [
          { name: 'terminal', test: /\/node_modules\/@xterm\// },
          // Keep the common editor together, without pulling the on-demand language packages into it.
          { name: 'editor', test: /\/node_modules\/(?:@codemirror\/(?:view|state|language|commands|autocomplete|search)|@lezer\/(?:common|highlight)|crelt|style-mod|w3c-keyname)\// },
        ] } },
      },
    },
  };
});

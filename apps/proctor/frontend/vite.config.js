import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig(({ command, mode }) => {
  if (command === 'build' && mode !== 'production') throw new Error('Shipping builds require production mode.');
  return {
    plugins: [svelte()], base: './',
    server: { fs: { allow: [fileURLToPath(new URL('.', import.meta.url)), fileURLToPath(new URL('../../../shared/theme', import.meta.url)), fileURLToPath(new URL('../../../shared/exam', import.meta.url))] } },
    resolve: { alias: { '$proctor-api': fileURLToPath(new URL(mode === 'ui-test' ? './tests/fake-api.js' : './src/api.js', import.meta.url)) } },
    build: { rolldownOptions: { external: ['/wails/runtime.js'] } },
  };
});

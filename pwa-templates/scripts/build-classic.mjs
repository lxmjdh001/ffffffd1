// Bundle the existing emitted JS only. Do not copy public images, process CSS,
// change application code, or require CORS configuration on the shared R2 bucket.
import { build } from 'vite';
import { readFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const templates = resolve(root, 'internal/platform/templates/pwa-landing');
const manifest = JSON.parse(await readFile(resolve(templates, '.vite/manifest.json'), 'utf8'));
for (const key of ['google-play/src/main.ts', 'custom-app/src/main.ts']) {
  const entry = manifest[key].file;
  await build({
    configFile: false,
    publicDir: false,
    build: {
      outDir: resolve(root, 'internal/platform/templates/landing-classic'),
      emptyOutDir: key === 'google-play/src/main.ts',
      minify: 'esbuild',
      target: 'es2020',
      lib: { entry: resolve(templates, entry), name: 'D1Landing', formats: ['iife'], fileName: () => entry },
      rollupOptions: { output: { inlineDynamicImports: true } },
    },
  });
}

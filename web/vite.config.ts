import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import { homeMessages } from './scripts/home-messages.ts';
import { homePrerender } from './scripts/home-prerender.ts';
import { canonicalHomeURL, DEFAULT_SEO_PUBLIC_URL } from './src/home-seo.ts';
const localBackend = { target: 'http://localhost:8088', changeOrigin: true, headers: { Origin: 'http://localhost:8088' } };
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', 'SEO_');
  const publicURL = canonicalHomeURL(env.SEO_PUBLIC_URL || DEFAULT_SEO_PUBLIC_URL);
  return { plugins: [homeMessages(), react(), homePrerender(publicURL)], define: { __SEO_PUBLIC_URL__: JSON.stringify(publicURL) }, server: { host: '127.0.0.1', port: 5178, strictPort: true, proxy: { '/api': localBackend, '/a': localBackend, '/d': localBackend, '/sdk': localBackend } }, build: { chunkSizeWarningLimit: 1500 } };
});

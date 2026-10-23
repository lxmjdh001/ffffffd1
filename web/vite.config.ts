import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
const localBackend = { target: 'http://localhost:8088', changeOrigin: true, headers: { Origin: 'http://localhost:8088' } };
export default defineConfig({ plugins: [react()], server: { host: '127.0.0.1', port: 5178, strictPort: true, proxy: { '/api': localBackend, '/a': localBackend, '/d': localBackend, '/sdk': localBackend } }, build: { chunkSizeWarningLimit: 1500 } });

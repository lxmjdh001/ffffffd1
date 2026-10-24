import {defineConfig} from 'vite';
import vue from '@vitejs/plugin-vue';
import {resolve} from 'node:path';
export default defineConfig({define:{__INTLIFY_JIT_COMPILATION__:true,__INTLIFY_DROP_MESSAGE_COMPILER__:false,__VUE_I18N_LEGACY_API__:false,__VUE_I18N_FULL_INSTALL__:true,__INTLIFY_PROD_DEVTOOLS__:false},plugins:[vue({template:{transformAssetUrls:{includeAbsolute:false}}})],base:'/pwa-template-assets/',publicDir:'google-play/public/assets',build:{outDir:'../internal/platform/templates/pwa-landing',emptyOutDir:true,manifest:true,rollupOptions:{input:{google_play:resolve('google-play/src/main.ts'),custom:resolve('custom-app/src/main.ts')},output:{assetFileNames:'[name]-[hash][extname]',entryFileNames:'[name]-[hash].js',chunkFileNames:'shared-[hash].js'}}}});

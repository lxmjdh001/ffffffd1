import { configDefaults, defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config.ts';

export default mergeConfig(viteConfig, defineConfig({
  test: {
    environment: 'jsdom',
    // macOS creates AppleDouble metadata files on external workspace drives.
    exclude: [...configDefaults.exclude, '**/._*'],
  },
}));

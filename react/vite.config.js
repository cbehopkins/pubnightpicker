import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      // injectManifest: we write our own SW (src/sw.js) and the plugin
      // injects a versioned precache manifest into it at build time.
      // This lets us combine Workbox caching with our existing push logic.
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.js',
      outDir: 'build',
      // We maintain public/manifest.json ourselves — don't let the plugin
      // generate or overwrite it.
      manifest: false,
      // The SW is intentionally disabled in dev to prevent stale cached
      // assets interfering with active development. To test offline/caching
      // behaviour locally, temporarily set enabled: true, but remember to
      // unregister the SW in DevTools > Application > Service Workers
      // afterwards so the cache doesn't persist across dev sessions.
      devOptions: {
        enabled: false,
      },
      injectManifest: {
        // Precache only the app shell (JS bundles, CSS, HTML entry point, fonts).
        // Images are intentionally excluded here — they are handled by the
        // runtime CacheFirst strategy in src/sw.js which caches them on first
        // use. This keeps the precache small (~1 MB vs ~4 MB) so the initial
        // SW install is not a heavy download for users.
        globPatterns: ['**/*.{js,css,html,woff,woff2}'],
      },
    }),
  ],
  test: {
    globalSetup: "./src/test-setup/emulatorGlobalSetup.js",
  },
  server: {
    port: 3000,
    open: true
  },
  build: {
    outDir: 'build',
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('/node_modules/firebase/') || id.includes('/node_modules/@firebase/')) {
            return 'firebase';
          }
        },
      },
    },
  },
  esbuild: {
    loader: 'jsx',
    include: /src\/.*\.jsx?$/,
    exclude: []
  },
  optimizeDeps: {
    esbuildOptions: {
      loader: {
        '.js': 'jsx',
      },
    }
  }
})

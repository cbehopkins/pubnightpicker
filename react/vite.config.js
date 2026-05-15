import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.js',
      // We maintain public/manifest.json ourselves — don't let the plugin
      // generate or overwrite it.
      manifest: false,
      // In dev mode the SW is served as a virtual ES module so push
      // registration succeeds. type: 'module' is required because src/sw.js
      // uses ES imports (workbox-precaching etc.).
      // outDir is intentionally omitted — the plugin defaults to Vite's own
      // build.outDir. Hardcoding it caused the dev virtual SW to be served
      // with the wrong MIME type (text/html 404).
      devOptions: {
        enabled: true,
        type: 'module',
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
    globals: true,
    setupFiles: ["./src/test-setup/vitest-setup.js"],
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

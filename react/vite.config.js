import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    open: true
  },
  build: {
    outDir: 'build',
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return;
          }
          if (id.includes('/node_modules/firebase/') || id.includes('/node_modules/@firebase/')) {
            return 'firebase';
          }
          if (id.includes('/node_modules/react/') || id.includes('/node_modules/react-dom/')) {
            return 'react-core';
          }
          if (id.includes('/node_modules/react-router/') || id.includes('/node_modules/react-router-dom/') || id.includes('/node_modules/@remix-run/router/')) {
            return 'router';
          }
          if (id.includes('/node_modules/react-markdown/') || id.includes('/node_modules/remark-gfm/') || id.includes('/node_modules/micromark/') || id.includes('/node_modules/mdast-util-')) {
            return 'markdown';
          }
          return 'vendor';
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

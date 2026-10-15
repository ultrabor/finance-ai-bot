import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  // Mantine + React land in one ~600 kB chunk; acceptable for a single-page admin panel.
  build: { chunkSizeWarningLimit: 800 },
  server: {
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY || 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});

import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@apirequest/i18n': fileURLToPath(new URL('./src/i18n', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    clearMocks: true,
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          react: ['react', 'react-dom'],
          query: ['@tanstack/react-query'],
          editor: ['@uiw/react-codemirror'],
          'editor-languages': ['@codemirror/lang-json', '@codemirror/lang-javascript'],
        },
      },
    },
  },
});

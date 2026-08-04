import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

function manualChunks(id: string) {
  const moduleId = id.replaceAll('\\', '/');
  if (moduleId.includes('/node_modules/react/') ||
      moduleId.includes('/node_modules/react-dom/') ||
      moduleId.includes('/node_modules/scheduler/')) {
    return 'react';
  }
  if (moduleId.includes('/node_modules/@tanstack/')) {
    return 'query';
  }
  if (moduleId.includes('/node_modules/@codemirror/lang-') ||
      moduleId.includes('/node_modules/@lezer/javascript/') ||
      moduleId.includes('/node_modules/@lezer/json/')) {
    return 'editor-languages';
  }
  if (moduleId.includes('/node_modules/@uiw/react-codemirror/') ||
      moduleId.includes('/node_modules/@codemirror/') ||
      moduleId.includes('/node_modules/@lezer/')) {
    return 'editor';
  }
}

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
        manualChunks,
      },
    },
  },
});

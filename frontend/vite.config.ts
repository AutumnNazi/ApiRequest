import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';
import { writeFileSync } from 'node:fs';

// main.go 的 //go:embed all:frontend/dist 要求该目录在干净克隆后就存在且非空，
// 因此仓库跟踪了 dist/gitkeep。但 vite 默认 emptyOutDir 会在每次构建时清空
// 目录连带删掉它，让工作区凭空多出一条删除记录。构建收尾时重建即可，
// 同时保留 emptyOutDir 清理旧 chunk 的行为。
function keepEmbedPlaceholder() {
  return {
    name: 'apirequest:keep-embed-placeholder',
    closeBundle() {
      writeFileSync(fileURLToPath(new URL('./dist/gitkeep', import.meta.url)), '');
    },
  };
}

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
  plugins: [react(), keepEmbedPlaceholder()],
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

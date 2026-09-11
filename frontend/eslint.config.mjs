// ESLint flat config：保守集——与 golangci.yml 同一策略，
// 先让"零告警"成为可执行门槛，按需再扩规则。
import js from '@eslint/js';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  // 脚手架/绑定生成物不参与
  { ignores: ['dist', 'wailsjs', 'coverage', 'node_modules'] },
  // JS 基线（含 .mjs/.cjs 配置文件）
  js.configs.recommended,
  // TS 基线：recommended 关掉与 tsc 重复的类型检查项（tsc --noEmit 已是 CI 门槛）
  ...tseslint.configs.recommended,
  {
    files: ['**/*.ts', '**/*.tsx'],
    rules: {
      // tsc --noEmit + noUnusedLocals/noUnusedParameters 已覆盖，避免双报
      '@typescript-eslint/no-unused-vars': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
      // 项目惯例：测试与工具层允许 require/namespace import（vitest mock 工厂）
      '@typescript-eslint/no-require-imports': 'off',
    },
  },
  // React hooks：真正的运行期 bug 捕手（rules-of-hooks 是硬错误）
  {
    files: ['**/*.tsx', '**/*.ts'],
    plugins: {
      'react-hooks': (await import('eslint-plugin-react-hooks')).default,
    },
    rules: {
      'react-hooks/rules-of-hooks': 'error',
      // exhaustive-deps 对事件桥/store 订阅模式误报多，列成待清偿专项再开
      'react-hooks/exhaustive-deps': 'off',
    },
  },
);

import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist', 'node_modules', 'build']),
  {
    files: ['**/*.{ts,tsx}'],
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      // Override react-hooks rules to be less strict
      'react-hooks/rules-of-hooks': 'error',  // Keep this as error (critical)
      'react-hooks/exhaustive-deps': 'warn',
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/refs': 'warn',
      'react-hooks/purity': 'warn',
      'react-hooks/static-components': 'warn',
      'react-hooks/preserve-manual-memoization': 'warn',
      // Disable react-refresh rule
      'react-refresh/only-export-components': 'off',
      // Downgrade no-explicit-any to warning (too many to fix at once)
      '@typescript-eslint/no-explicit-any': 'warn',
      // Disable non-critical rules
      'no-useless-escape': 'warn',
      'no-empty': 'warn',
      'no-constant-binary-expression': 'warn',
      '@typescript-eslint/no-non-null-asserted-optional-chain': 'warn',
      'no-prototype-builtins': 'warn',
      // Disable no-unused-vars errors (downgrade to warn)
      '@typescript-eslint/no-unused-vars': 'warn',
    },
  },
])

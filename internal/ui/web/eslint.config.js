import js from '@eslint/js'
import prettier from 'eslint-config-prettier/flat'
import { defineConfig, globalIgnores } from 'eslint/config'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'
import tseslint from 'typescript-eslint'

// Type-aware strict rules; Prettier last so formatting never fights linting.
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.strictTypeChecked,
      tseslint.configs.stylisticTypeChecked,
      reactHooks.configs.flat['recommended-latest'],
      reactRefresh.configs.vite,
      prettier,
    ],
    languageOptions: {
      ecmaVersion: 2023,
      globals: globals.browser,
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
  },
  {
    // Vendored shadcn code: `shadcn add` overwrites edits, so only stylistic rules are relaxed here.
    // Correctness rules (hooks, unsafe types, floating promises) still apply.
    files: ['src/components/ui/**/*.tsx'],
    rules: {
      'react-refresh/only-export-components': 'off',
      '@typescript-eslint/consistent-type-definitions': 'off',
      '@typescript-eslint/restrict-template-expressions': 'off',
      '@typescript-eslint/no-confusing-void-expression': 'off',
    },
  },
  {
    // Vendored shadcn chart: Recharts types its tooltip payloads as `any`. Only this file.
    files: ['src/components/ui/chart.tsx'],
    rules: {
      '@typescript-eslint/no-unsafe-assignment': 'off',
      '@typescript-eslint/no-unsafe-member-access': 'off',
      '@typescript-eslint/no-unsafe-argument': 'off',
      '@typescript-eslint/no-unnecessary-condition': 'off',
      '@typescript-eslint/no-unnecessary-type-assertion': 'off',
    },
  },
  {
    // API paths live only in src/app/endpoints.ts, so a backend route change is a one-line edit.
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/app/endpoints.ts'],
    rules: {
      'no-restricted-syntax': [
        'error',
        {
          selector: 'Literal[value=/^\\/(v1|ready|health)(\\/|$)/]',
          message: 'Import the path from API in src/app/endpoints.ts instead of writing it here.',
        },
        {
          selector: 'TemplateElement[value.raw=/^\\/(v1|ready|health)(\\/|$)/]',
          message: 'Import the path from API in src/app/endpoints.ts instead of writing it here.',
        },
      ],
    },
  },
])

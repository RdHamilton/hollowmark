import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist', '.vite', '**/*.test.{ts,tsx}', '**/test/**', 'src/types/models.ts', 'coverage/**', 'storybook-static/**']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    rules: {
      // ADR-084 Fitness Function: colon-vocabulary SSE event guard.
      // These event names had zero BFF server-side emitters since the Wails→REST migration.
      // The readmodel.updated subscription (ADR-084) is the correct replacement.
      // Also guards against re-introducing the dot-vocabulary race (match.completed).
      'no-restricted-syntax': [
        'error',
        {
          selector: "CallExpression[callee.name='EventsOn'][arguments.0.value=/^(stats|quest|draft|rank|collection):updated$/]",
          message: "Dead colon-vocabulary SSE listener (ADR-084). Use useReadModelUpdates with the appropriate domain callback instead.",
        },
        {
          selector: "CallExpression[callee.name='EventsOn'][arguments.0.value=/^(download|task):(progress|complete|error)$/]",
          message: "Dead colon-vocabulary SSE listener (ADR-084). Download/task progress is driven via context API, not SSE events.",
        },
        {
          selector: "CallExpression[callee.name='EventsOn'][arguments.0.value='match.completed']",
          message: "match.completed races the projection layer (ADR-084 root cause 1). Use useReadModelUpdates with onMatches instead.",
        },
        {
          selector: "CallExpression[callee.name='EventsEmit'][arguments.0.value=/^(stats|quest|draft|rank|collection):updated$/]",
          message: "Dead colon-vocabulary SSE emit (ADR-084). Use EventsEmit('readmodel.updated', { domains: [...] }) instead.",
        },
      ],
    },
  },
  {
    // Storybook configuration & tooling files are not part of the app bundle,
    // so Vite Fast Refresh does not apply to them. The Clerk mock in
    // .storybook/clerk-mock.tsx must mirror @clerk/react's export shape, which
    // legitimately mixes component and non-component exports.
    files: ['.storybook/**/*.{ts,tsx}'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
])

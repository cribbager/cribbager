import js from '@eslint/js';
import globals from 'globals';

// Conservative: ESLint's recommended ruleset, with the right globals for the
// browser client vs. the Node smoke scripts/tests. No style rules.
export default [
  { ignores: ['node_modules/**'] },
  js.configs.recommended,
  {
    // Honor the _-prefix convention for intentionally-unused vars/args.
    rules: {
      'no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    },
  },
  {
    files: ['public/**/*.js'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser },
    },
  },
  {
    // Smoke scripts are Node, but their page.evaluate() callbacks run in the
    // browser — so they legitimately reference browser globals too.
    files: ['scripts/**/*.mjs', 'test/**/*.js'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.node, ...globals.browser },
    },
  },
];

import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";

export default [
    {
        ignores: ["build/**", "dev-dist/**", "node_modules/**"],
    },
    js.configs.recommended,
    {
        plugins: { "react-hooks": reactHooks },
        rules: {
            ...reactHooks.configs.recommended.rules,
            // This rule flags intentional "reset state when dep changes" patterns
            // which are common and valid in this codebase.
            "react-hooks/set-state-in-effect": "off",
            // Downgrade to warn — legacy `import React` and minor unused vars
            // are noise; the important violations (rules-of-hooks) stay as errors.
            "no-unused-vars": "warn",
        },
        languageOptions: {
            globals: { ...globals.browser, ...globals.es2022 },
            parserOptions: { ecmaFeatures: { jsx: true } },
        },
    },
    {
        // Test files and test infrastructure run in Node (via Vitest)
        files: ["**/*.test.js", "src/test-setup/**/*.js", "src/dbtools/*.permissions.test.js"],
        languageOptions: {
            globals: { ...globals.node, ...globals.browser, ...globals.es2022 },
        },
    },
];

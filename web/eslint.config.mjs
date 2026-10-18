import tseslint from "typescript-eslint";
import svelte from "eslint-plugin-svelte";

import peios from "../build/style/eslint-peios.mjs";

const ignored = [
    "**/node_modules/**",
    "**/.venv/**",
    "**/vendor/**",
    "**/generated/**",
    "**/build/**",
    "**/dist/**",
];

export default [
    { ignores: [...ignored, "**/.svelte-kit/**"] },
    ...svelte.configs["flat/base"],
    {
        files: ["**/*.{js,mjs,cjs,ts,tsx,svelte}"],
        plugins: { peios },
        linterOptions: { reportUnusedDisableDirectives: "error" },
        rules: {
            "peios/braces": "error",
            "no-dupe-args": "error",
            "no-duplicate-case": "error",
            "no-unreachable": "error",
            "no-empty": "error",
            "no-unused-vars": "error",
            "prefer-const": "error",
        },
    },
    {
        files: ["**/*.{ts,tsx,svelte}"],
        languageOptions: { parserOptions: { parser: tseslint.parser } },
        plugins: { "@typescript-eslint": tseslint.plugin },
        rules: {
            "no-unused-vars": "off",
            "@typescript-eslint/no-unused-vars": [
                "error",
                { argsIgnorePattern: "^_", varsIgnorePattern: "^_$" },
            ],
            "@typescript-eslint/no-explicit-any": "error",
        },
    },
    { files: ["**/*.{ts,tsx}"], languageOptions: { parser: tseslint.parser } },
    {
        files: ["**/*.svelte", "**/*.svelte.ts"],
        // Svelte props and derived values can change through the framework.
        rules: { "prefer-const": "off", "svelte/prefer-const": "error" },
    },
];

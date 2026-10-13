import { defineConfig } from "oxlint";

export default defineConfig({
  plugins: ["typescript", "react", "jsx-a11y", "import", "unicorn"],
  jsPlugins: ["eslint-plugin-compat"],
  options: {
    typeAware: true,
    reportUnusedDisableDirectives: "deny",
  },
  categories: {
    correctness: "error",
    suspicious: "error",
    perf: "warn",
  },
  env: {
    browser: true,
  },
  rules: {
    "compat/compat": "error",
    "react/button-has-type": "error",
    "react/no-unknown-property": "error",
    "react/jsx-no-target-blank": "error",
    "jsx-a11y/anchor-is-valid": "error",
    "jsx-a11y/click-events-have-key-events": "error",
    "jsx-a11y/interactive-supports-focus": "error",
    "jsx-a11y/label-has-associated-control": "error",
    "import/no-duplicates": "error",
    "typescript/no-floating-promises": [
      "error",
      {
        ignoreVoid: true,
      },
    ],
    "typescript/no-misused-promises": [
      "error",
      {
        checksVoidReturn: {
          arguments: false,
          attributes: false,
        },
      },
    ],
    "typescript/switch-exhaustiveness-check": "error",
  },
});

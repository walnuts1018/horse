import { defineConfig } from "oxlint";

export default defineConfig({
  jsPlugins: ["eslint-plugin-compat"],
  env: {
    browser: true,
  },
  rules: {
    "compat/compat": "error",
  },
});

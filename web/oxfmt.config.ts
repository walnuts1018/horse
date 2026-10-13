import { defineConfig } from "oxfmt";

export default defineConfig({
  ignorePatterns: ["src/routeTree.gen.ts", "src/**/__generated__/**", ".output/**"],
});

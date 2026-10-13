export type AppPage =
  | "home"
  | "ledger"
  | "new"
  | "settlement"
  | "analysis"
  | "imports"
  | "settings";

const appPages = new Set<AppPage>([
  "home",
  "ledger",
  "new",
  "settlement",
  "analysis",
  "imports",
  "settings",
]);

export const isAppPage = (value: unknown): value is AppPage =>
  typeof value === "string" && appPages.has(value as AppPage);

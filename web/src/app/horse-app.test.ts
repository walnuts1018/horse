import { describe, expect, it, vi } from "vitest";

vi.mock("../shared/api", () => ({
  ApiError: class ApiError extends Error {},
  getViewer: vi.fn(),
}));
vi.mock("../entities/household", () => ({}));
vi.mock("../entities/purchase", () => ({ useCreateAndPostPurchase: vi.fn() }));
vi.mock("../shared/lib", () => ({}));
vi.mock("../shared/ui", () => ({}));
vi.mock("./graphql-error-boundary", () => ({ GraphQLErrorBoundary: () => null }));

import { shouldAutoNavigateAfterPurchase } from "./horse-app";

describe("purchase outbox navigation", () => {
  it("新しい入力がある場合は先行送信の完了で帳簿へ遷移しない", () => {
    expect(
      shouldAutoNavigateAfterPurchase({
        queuedKey: "purchase-1",
        completedKey: "purchase-1",
        currentPage: "new",
        hasUnsavedChanges: true,
      }),
    ).toBe(false);
  });

  it("入力がなく同じ送信の完了時は従来どおり帳簿へ遷移する", () => {
    expect(
      shouldAutoNavigateAfterPurchase({
        queuedKey: "purchase-1",
        completedKey: "purchase-1",
        currentPage: "new",
        hasUnsavedChanges: false,
      }),
    ).toBe(true);
  });
});

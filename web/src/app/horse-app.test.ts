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

import { ApiError } from "../shared/api";
import { shouldAutoNavigateAfterPurchase } from "./horse-app";
import {
  isRetryablePurchaseSendError,
  purchaseOutboxRetryDelay,
  refreshHouseholdAfterPurchase,
} from "./model/use-purchase-outbox-sender";

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

describe("purchase outbox refresh", () => {
  it("購入一覧の再取得に失敗しても購入確定の成功扱いを維持する", async () => {
    const refreshHousehold = vi.fn(async () => {
      throw new Error("refresh failed");
    });

    await expect(refreshHouseholdAfterPurchase(refreshHousehold, "household-1")).resolves.toBe(
      false,
    );
    expect(refreshHousehold).toHaveBeenCalledWith("household-1");
  });
});

describe("purchase outbox retry policy", () => {
  it("通信障害とサーバー障害を再試行し、入力エラーは失敗として扱う", () => {
    const serverError = Object.assign(new ApiError("server error"), { status: 503 });
    const validationError = Object.assign(new ApiError("invalid input"), { status: 400 });

    expect(isRetryablePurchaseSendError(new TypeError("network error"))).toBe(true);
    expect(isRetryablePurchaseSendError(serverError)).toBe(true);
    expect(isRetryablePurchaseSendError(validationError)).toBe(false);
    expect(isRetryablePurchaseSendError(new Error("unexpected error"))).toBe(false);
  });

  it("再試行間隔を指数的に延ばして5分で止める", () => {
    expect(purchaseOutboxRetryDelay(0)).toBe(1000);
    expect(purchaseOutboxRetryDelay(2)).toBe(4000);
    expect(purchaseOutboxRetryDelay(20)).toBe(5 * 60 * 1000);
  });
});

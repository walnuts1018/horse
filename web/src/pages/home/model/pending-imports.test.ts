import { describe, expect, it } from "vitest";

import { pendingImportMessage, summarizePendingImports } from "./pending-imports";

describe("pending imports home status", () => {
  it("SUCCEEDED文書を確認待ちとして表示する", () => {
    const status = summarizePendingImports(["SUCCEEDED"], false);

    expect(pendingImportMessage(status)).toBe("確認待ち 1件");
  });

  it("FAILED文書を再解析が必要な状態として表示する", () => {
    const status = summarizePendingImports(["FAILED"], false);

    expect(pendingImportMessage(status)).toBe("解析失敗 1件");
  });

  it("処理中の文書は解析中として表示する", () => {
    const status = summarizePendingImports(["RUNNING"], false);

    expect(pendingImportMessage(status)).toBe("AI解析中");
  });

  it("100件を超える確認対象は不完全な件数を出さず状態を表示する", () => {
    const status = summarizePendingImports(["SUCCEEDED"], true);

    expect(pendingImportMessage(status)).toBe("確認・再解析が必要");
  });
});

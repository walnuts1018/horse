import { afterEach, describe, expect, it, vi } from "vitest";

import { readDocumentMatches } from "./document-matches";

afterEach(() => vi.unstubAllGlobals());

describe("readDocumentMatches", () => {
  it("照合候補がまだない404を空結果として返す", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 404 }));
    vi.stubGlobal("fetch", fetch);

    await expect(readDocumentMatches("document/1", "row:120")).resolves.toEqual({
      observations: [],
      truncated: false,
    });
    expect(fetch).toHaveBeenCalledWith(
      "/documents/document%2F1/matches?sourceReference=row%3A120",
      { credentials: "include" },
    );
  });

  it("404以外の失敗をエラーとして返す", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 503 })));

    await expect(readDocumentMatches("document-1")).rejects.toThrow(
      "照合候補を取得できませんでした (503)",
    );
  });
});

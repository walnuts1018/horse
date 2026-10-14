import { afterEach, describe, expect, it, vi } from "vitest";

import { readObservationMatches } from "./matches";

afterEach(() => vi.unstubAllGlobals());

describe("readObservationMatches", () => {
  it("選択中の明細行を指定して照合候補を取得する", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          observations: [
            {
              sourceReference: "row:120",
              matches: [
                {
                  documentId: "document-2",
                  sourceReference: "row:4",
                  state: "POSSIBLE",
                  reasons: ["金額一致"],
                },
              ],
            },
          ],
          truncated: false,
        }),
      ),
    );
    vi.stubGlobal("fetch", fetch);

    await expect(readObservationMatches("document/1", "row:120")).resolves.toEqual([
      {
        documentId: "document-2",
        sourceReference: "row:4",
        state: "POSSIBLE",
        reasons: ["金額一致"],
      },
    ]);
    expect(fetch).toHaveBeenCalledWith(
      "/documents/document%2F1/matches?sourceReference=row%3A120",
      { credentials: "include" },
    );
  });
});

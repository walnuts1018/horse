import { describe, expect, it, vi } from "vitest";

import { handleUnsavedChangesBeforeUnload } from "./use-unsaved-changes";

describe("handleUnsavedChangesBeforeUnload", () => {
  it("未保存の変更がある場合は離脱をキャンセルする", () => {
    const preventDefault = vi.fn();
    const event = { preventDefault };

    handleUnsavedChangesBeforeUnload(event, () => true);

    expect(preventDefault).toHaveBeenCalledOnce();
  });

  it("未保存の変更がない場合は離脱を許可する", () => {
    const preventDefault = vi.fn();
    const event = { preventDefault };

    handleUnsavedChangesBeforeUnload(event, () => false);

    expect(preventDefault).not.toHaveBeenCalled();
  });
});

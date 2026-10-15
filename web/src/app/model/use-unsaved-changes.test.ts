import { describe, expect, it, vi } from "vitest";

import { handleUnsavedChangesBeforeUnload } from "./use-unsaved-changes";

describe("handleUnsavedChangesBeforeUnload", () => {
  it("未保存の変更がある場合はブラウザーの離脱確認を要求する", () => {
    const event = { preventDefault: vi.fn(), returnValue: undefined as string | undefined };

    handleUnsavedChangesBeforeUnload(event, () => true);

    expect(event.preventDefault).toHaveBeenCalledOnce();
    expect(event.returnValue).toBe("");
  });

  it("未保存の変更がない場合は離脱確認を要求しない", () => {
    const event = { preventDefault: vi.fn(), returnValue: undefined as string | undefined };

    handleUnsavedChangesBeforeUnload(event, () => false);

    expect(event.preventDefault).not.toHaveBeenCalled();
    expect(event.returnValue).toBeUndefined();
  });
});

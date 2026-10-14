import { describe, expect, it } from "vitest";

import { createPurchaseDraftWriteQueue } from "./use-purchase-draft";

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

describe("purchase draft write queue", () => {
  it("waits for an older write before persisting the latest draft", async () => {
    const enqueue = createPurchaseDraftWriteQueue();
    const writes: string[] = [];
    const olderWrite = deferred<void>();

    const older = enqueue(async () => {
      writes.push("older:start");
      await olderWrite.promise;
      writes.push("older:finish");
    });
    const latest = enqueue(async () => {
      writes.push("latest");
    });

    await Promise.resolve();
    expect(writes).toEqual(["older:start"]);

    olderWrite.resolve();
    await Promise.all([older, latest]);

    expect(writes).toEqual(["older:start", "older:finish", "latest"]);
  });
});

import type { Meta, StoryObj } from "@storybook/react-vite";

import { useEffect, useState } from "react";
import { expect, userEvent, waitFor, within } from "storybook/test";

import type { DocumentReview } from "../model/imports";

import { DocumentConversation } from "./document-conversation";

const meta = {
  title: "Pages/Imports/Document conversation",
  parameters: { layout: "centered" },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

function FailedConversation() {
  const [mountKey, setMountKey] = useState(0);
  const [document, setDocument] = useState<DocumentReview>({
    id: "story-document",
    status: "queued",
    model: "gemini-3.8-flash",
    result: { receipt: { merchant: "喫茶店", total: 1200 } },
  });

  useEffect(() => {
    sessionStorage.removeItem("document-interaction:story-document");
    const originalFetch = window.fetch;
    window.fetch = async () => new Response(null, { status: 202 });
    return () => {
      window.fetch = originalFetch;
    };
  }, []);

  return (
    <div style={{ width: "min(640px, 90vw)" }}>
      <button type="button" onClick={() => setMountKey((current) => current + 1)}>
        画面を再読み込み
      </button>
      <DocumentConversation
        key={mountKey}
        document={document}
        onError={(message) => window.alert(message)}
        onUpdated={() =>
          setDocument((current) => ({
            ...current,
            status: "failed",
            errorCode: "rate_limited",
          }))
        }
      />
    </div>
  );
}

export const FailedInstructionCanBeResent: Story = {
  render: () => <FailedConversation />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(canvas.getByLabelText("AIに追加指示"), "店名を確認してください");
    await userEvent.click(canvas.getByRole("button", { name: "このモデルで再確認" }));
    await waitFor(async () => {
      await expect(canvas.getByText("店名を確認してください")).toBeTruthy();
      await expect(canvas.getByRole("button", { name: "同じ指示を再送" })).toBeTruthy();
      await expect(
        canvas.getByRole("button", { name: "Gemini 3.1 Pro Previewで同じ指示を再送・有料" }),
      ).toBeTruthy();
    });
    await userEvent.click(canvas.getByRole("button", { name: "画面を再読み込み" }));
    await expect(canvas.getByText("店名を確認してください")).toBeTruthy();
    await expect(canvas.getByRole("button", { name: "同じ指示を再送" })).toBeTruthy();
  },
};

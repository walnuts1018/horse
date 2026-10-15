import type { Meta, StoryObj } from "@storybook/react-vite";

import { useEffect, useState } from "react";
import { expect, userEvent, waitFor, within } from "storybook/test";

import type { DocumentReview } from "../model/imports";

import { storeReceiptExtractionSnapshot } from "../model/extraction-diff";
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
    const originalFetch = window.fetch;
    window.fetch = async () =>
      Response.json({ jobId: "story-interaction", status: "queued" }, { status: 202 });
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
            pendingInteraction: {
              jobId: "story-interaction",
              status: "failed",
              message: "店名を確認してください",
              model: "gemini-3.8-flash",
              baseJobId: "story-base-job",
            },
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

const extractionChangeDocument: DocumentReview = {
  id: "updated-extraction-story",
  jobId: "job-new",
  status: "succeeded",
  model: "gemini-3.8-flash",
  result: {
    receipt: {
      total: 1280,
      items: [
        {
          name: "コーヒー豆",
          printedAmount: 600,
          grossAmount: 600,
          categoryHint: "食品",
          taxGroup: "8%",
          confidence: 1,
          verification: "verified",
        },
      ],
      taxGroups: [{ id: "tax-8", numerator: 8, denominator: 100, printedTax: 96 }],
      payments: [{ method: "クレジットカード", cardLast4: "1234", amount: 1280 }],
    },
    conversation: [
      { role: "user", text: "品目のカテゴリと金額を確認してください" },
      { role: "assistant", text: "原本の表示に合わせて更新しました。" },
    ],
  },
};

function UpdatedExtraction() {
  return (
    <div style={{ width: "min(640px, 90vw)" }}>
      <DocumentConversation
        document={extractionChangeDocument}
        onError={() => undefined}
        onUpdated={() => undefined}
      />
    </div>
  );
}

export const ExtractionChanges: Story = {
  render: () => <UpdatedExtraction />,
  loaders: [
    async () => {
      storeReceiptExtractionSnapshot(extractionChangeDocument.id, {
        jobId: "job-old",
        nextJobId: "job-new",
        receipt: {
          total: 1100,
          items: [
            {
              name: "コーヒー",
              printedAmount: 500,
              grossAmount: 500,
              categoryHint: "飲料",
              taxGroup: "10%",
              confidence: 1,
              verification: "verified",
            },
          ],
          taxGroups: [{ id: "tax-10", numerator: 10, denominator: 100, printedTax: 100 }],
          payments: [{ method: "現金", amount: 1100 }],
        },
      });
      return {};
    },
  ],
};

function BranchConversation() {
  const [document, setDocument] = useState<DocumentReview>({
    id: "story-branch-document",
    jobId: "story-job-v2",
    status: "succeeded",
    model: "gemini-3.8-flash",
    result: { receipt: { merchant: "喫茶店", total: 1200 } },
  });

  useEffect(() => {
    const originalFetch = window.fetch;
    window.fetch = async (input, init) => {
      const url = new URL(
        typeof input === "string" ? input : input instanceof URL ? input.href : input.url,
        window.location.origin,
      );
      if (url.pathname.endsWith("/extractions") && init?.method !== "POST") {
        const completed = window.sessionStorage.getItem("story-branch-complete") === "true";
        return Response.json({
          versions: [
            ...(completed
              ? [
                  {
                    id: "story-job-v3",
                    version: 3,
                    model: "gemini-3.8-flash",
                    createdAt: "2026-10-16T00:00:00Z",
                    conversationTurns: 2,
                  },
                ]
              : []),
            {
              id: "story-job-v2",
              version: 2,
              model: "gemini-3.8-flash",
              createdAt: "2026-10-15T00:00:00Z",
              conversationTurns: 2,
            },
            {
              id: "story-job-v1",
              version: 1,
              model: "gemini-3.8-flash",
              createdAt: "2026-10-14T00:00:00Z",
              conversationTurns: 1,
            },
          ],
        });
      }
      if (url.pathname.endsWith("/story-job-v3")) {
        return Response.json({
          id: "story-job-v3",
          version: 3,
          model: "gemini-3.8-flash",
          createdAt: "2026-10-16T00:00:00Z",
          conversationTurns: 2,
          result: {
            receipt: { merchant: "喫茶店", total: 1100 },
            conversation: [
              { role: "user", text: "前の会話の質問" },
              { role: "assistant", text: "前の会話の回答" },
              { role: "user", text: "前の版からもう一度確認してください" },
              { role: "assistant", text: "旧版を起点に再確認しました" },
            ],
          },
        });
      }
      if (url.pathname.endsWith("/story-job-v1")) {
        return Response.json({
          id: "story-job-v1",
          version: 1,
          model: "gemini-3.8-flash",
          createdAt: "2026-10-14T00:00:00Z",
          conversationTurns: 1,
          result: {
            receipt: { merchant: "喫茶店", total: 1000 },
            conversation: [
              { role: "user", text: "前の会話の質問" },
              { role: "assistant", text: "前の会話の回答" },
            ],
          },
        });
      }
      if (url.pathname.endsWith("/interaction") && init?.method === "POST") {
        window.sessionStorage.setItem("story-branch-request", String(init.body));
        return Response.json({ jobId: "story-job-v3", status: "queued" }, { status: 202 });
      }
      return new Response(null, { status: 404 });
    };
    return () => {
      window.fetch = originalFetch;
      window.sessionStorage.removeItem("story-branch-request");
      window.sessionStorage.removeItem("story-branch-complete");
      window.sessionStorage.removeItem("document-extraction:story-branch-document");
    };
  }, []);

  return (
    <div style={{ width: "min(640px, 90vw)" }}>
      <DocumentConversation
        document={document}
        onError={() => undefined}
        onUpdated={() => {
          setDocument((current) => ({ ...current, jobId: "story-job-v3", status: "queued" }));
        }}
      />
      <button
        type="button"
        onClick={() => {
          window.sessionStorage.setItem("story-branch-complete", "true");
          setDocument((current) => ({ ...current, status: "succeeded" }));
        }}
      >
        ジョブを完了する
      </button>
    </div>
  );
}

export const PreviousVersionCanStartABranch: Story = {
  render: () => <BranchConversation />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: "過去の抽出版から分岐" }));
    const versionPicker = await canvas.findByLabelText("再確認の起点");
    await userEvent.selectOptions(versionPicker, "story-job-v1");
    await expect(await canvas.findByText("前の会話の質問")).toBeTruthy();
    await userEvent.type(
      canvas.getByLabelText("AIに追加指示"),
      "前の版からもう一度確認してください",
    );
    await userEvent.click(canvas.getByRole("button", { name: "このモデルで再確認" }));
    await waitFor(() => {
      const request = window.sessionStorage.getItem("story-branch-request");
      expect(request).not.toBeNull();
      expect(JSON.parse(request ?? "{}")).toMatchObject({
        message: "前の版からもう一度確認してください",
        baseJobId: "story-job-v1",
      });
    });
    await userEvent.click(canvas.getByRole("button", { name: "ジョブを完了する" }));
    await expect(await canvas.findByText("旧版を起点に再確認しました")).toBeTruthy();
  },
};

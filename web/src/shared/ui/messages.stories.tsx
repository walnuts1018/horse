import type { Meta, StoryObj } from "@storybook/react-vite";

import { Inbox } from "lucide-react";
import { fn } from "storybook/test";

import { EmptyState, InlineError } from "./messages";

const onCreatePurchase = fn();

const meta = {
  title: "Shared UI/Messages",
  parameters: {
    layout: "centered",
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const EmptyPurchaseList: Story = {
  render: () => (
    <section className="panel" style={{ width: "min(420px, 90vw)", padding: "1rem" }}>
      <EmptyState
        icon={<Inbox aria-hidden="true" />}
        title="買い物はまだありません"
        action="買い物を記録"
        onClick={onCreatePurchase}
      />
    </section>
  ),
};

export const InlineValidationError: Story = {
  render: () => (
    <div style={{ width: "min(420px, 90vw)" }}>
      <label style={{ display: "grid", gap: "0.4rem" }}>
        店名
        <input aria-invalid="true" defaultValue="" />
      </label>
      <InlineError message="店名を入力してください。" />
    </div>
  ),
};

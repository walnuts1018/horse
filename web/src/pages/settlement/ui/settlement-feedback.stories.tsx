import type { Meta, StoryObj } from "@storybook/react-vite";

import { fn } from "storybook/test";

import { SettlementFeedback } from "./settlement-feedback";

import "./settlement-page.css";

const meta = {
  title: "Pages/Settlement/Feedback",
  component: SettlementFeedback,
  args: { error: "", success: "", loadError: "", onRetryLoad: fn() },
  parameters: { layout: "padded" },
} satisfies Meta<typeof SettlementFeedback>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {};

export const Saved: Story = {
  args: { success: "2026年6月分の入金を記録しました" },
};

export const SaveError: Story = {
  args: { error: "入金を記録できませんでした。時間をおいて再度お試しください。" },
};

export const LoadError: Story = {
  args: { loadError: "精算情報を読み込めませんでした。", onRetryLoad: fn() },
};

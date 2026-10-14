import type { Meta, StoryObj } from "@storybook/react-vite";

import { Notice } from "./notice";

const meta = {
  title: "Shared UI/Notice",
  component: Notice,
  args: {
    message: "買い物を帳簿に追加しました",
  },
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof Notice>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Success: Story = {
  args: { kind: "success" },
};

export const Error: Story = {
  args: {
    kind: "error",
    message: "変更を保存できませんでした。入力内容を確認してください。",
  },
};

export const Warning: Story = {
  args: {
    kind: "warning",
    message: "負担割合の合計が100%になっていません。",
  },
};

export const Information: Story = {
  args: {
    kind: "info",
    message: "新しい項目が追加されています。",
  },
};

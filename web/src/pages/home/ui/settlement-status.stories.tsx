import type { Meta, StoryObj } from "@storybook/react-vite";

import { SettlementStatus } from "./settlement-status";

import "./home-page.css";

const meta = {
  title: "Pages/Home/Settlement status",
  component: SettlementStatus,
  decorators: [
    (Story) => (
      <div className="home-page">
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof SettlementStatus>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Open: Story = {
  args: { state: "open" },
};

export const Closed: Story = {
  args: { state: "closed" },
};

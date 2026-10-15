import type { Meta, StoryObj } from "@storybook/react-vite";

import { AnalysisSummary } from "./analysis-summary";

import "./analysis-page.css";

const meta = {
  title: "Pages/Analysis/Summary",
  component: AnalysisSummary,
  decorators: [
    (Story) => (
      <div className="analysis-page">
        <Story />
      </div>
    ),
  ],
  args: {
    annual: false,
    annualYear: 2026,
    periodCount: 3,
    total: 128_450,
    dimension: "category",
    peakRow: { id: "food", name: "食費", amount: 64_200, count: 48 },
  },
} satisfies Meta<typeof AnalysisSummary>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Quarterly: Story = {};

export const Annual: Story = {
  args: { annual: true, periodCount: 12, total: 486_220, annualYear: 2025 },
};

export const NoPurchases: Story = {
  args: { total: 0, periodCount: 3, peakRow: undefined },
};

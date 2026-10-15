import type { Meta, StoryObj } from "@storybook/react-vite";

import { AnalysisBreakdown } from "./analysis-breakdown";

import "./analysis-page.css";

const meta = {
  title: "Pages/Analysis/Breakdown",
  component: AnalysisBreakdown,
  decorators: [
    (Story) => (
      <div className="analysis-page">
        <Story />
      </div>
    ),
  ],
  args: {
    dimension: "category",
    monthly: [
      { month: "2026-04", amount: 132_500 },
      { month: "2026-05", amount: 118_200 },
      { month: "2026-06", amount: 145_900 },
    ],
    categorySeries: [
      { id: "food", name: "食費", colorIndex: 0 },
      { id: "daily", name: "日用品", colorIndex: 1 },
      { id: "other", name: "その他", colorIndex: 2 },
    ],
    remainingCategories: [],
    categoryAmountsByMonth: new Map([
      [
        "2026-04",
        new Map([
          ["food", 82_500],
          ["daily", 25_000],
          ["other", 25_000],
        ]),
      ],
      [
        "2026-05",
        new Map([
          ["food", 70_200],
          ["daily", 18_000],
          ["other", 30_000],
        ]),
      ],
      [
        "2026-06",
        new Map([
          ["food", 91_900],
          ["daily", 24_000],
          ["other", 30_000],
        ]),
      ],
    ]),
    max: 145_900,
    maxCategoryPositive: 145_900,
    maxCategoryNegative: 1,
    rows: [
      { id: "food", name: "食費", amount: 244_600, count: 74 },
      { id: "daily", name: "日用品", amount: 67_000, count: 19 },
      { id: "other", name: "その他", amount: 85_000, count: 27 },
    ],
    hasUnattributedPurchases: false,
  },
} satisfies Meta<typeof AnalysisBreakdown>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Categories: Story = {};

export const NoPurchases: Story = {
  args: {
    monthly: [],
    categorySeries: [],
    categoryAmountsByMonth: new Map(),
    rows: [],
    max: 1,
  },
};

export const Corrections: Story = {
  args: {
    dimension: "merchant",
    monthly: [
      { month: "2026-05", amount: 12_400 },
      { month: "2026-06", amount: -2_800 },
    ],
    categorySeries: [],
    categoryAmountsByMonth: new Map(),
    rows: [{ id: "refund", name: "返金・訂正", amount: 12_400, count: 2 }],
    max: 12_400,
  },
};

import type { Meta, StoryObj } from "@storybook/react-vite";

import type { LedgerEntry } from "../../../shared/api";

import { AnalysisLedgerPanel } from "./analysis-ledger-panel";

import "./analysis-page.css";

const entries: LedgerEntry[] = [
  {
    id: "funding-1",
    kind: "FUNDING",
    occurredAt: "2026-06-04T10:00:00Z",
    description: "共用プールへの入金",
    amountYen: 30_000,
    currency: "JPY",
  },
  {
    id: "advance-1",
    kind: "ADVANCE",
    occurredAt: "2026-06-09T10:00:00Z",
    description: "食材の立て替え",
    amountYen: 4_280,
    currency: "JPY",
  },
  {
    id: "repayment-1",
    kind: "REPAYMENT",
    occurredAt: "2026-06-15T10:00:00Z",
    description: "6月分の精算",
    amountYen: 4_280,
    currency: "JPY",
  },
];

const meta = {
  title: "Pages/Analysis/Ledger",
  component: AnalysisLedgerPanel,
  decorators: [
    (Story) => (
      <div className="analysis-page">
        <Story />
      </div>
    ),
  ],
  args: { entries, months: ["2026-06"] },
} satisfies Meta<typeof AnalysisLedgerPanel>;

export default meta;
type Story = StoryObj<typeof meta>;

export const RecentActivity: Story = {};

export const Empty: Story = { args: { entries: [], months: ["2026-06"] } };

import type { Meta, StoryObj } from "@storybook/react-vite";

import { useState } from "react";
import { fn } from "storybook/test";

import type { Household, Tag } from "../../../shared/api";

import { defaultLedgerSearch, type LedgerSearch } from "../model/ledger-search";
import { LedgerFilters } from "./ledger-filters";

import "./ledger-page.css";

const household: Household = {
  id: "household-1",
  name: "わが家",
  revision: 1,
  members: [
    { id: "member-1", displayName: "花子", role: "OWNER" },
    { id: "member-2", displayName: "太郎", role: "MEMBER" },
  ],
  pools: [
    {
      __typename: "SharedPool",
      id: "pool-shared",
      name: "共通",
      defaultShareWeight: 100,
      fundingShares: [],
      settlementPolicy: "BALANCE_TO_STANDARD",
      fundingRuleVersion: 1,
    },
  ],
  accounts: [{ id: "account-card", name: "家族カード", kind: "CARD", cardLast4: "1234" }],
  categories: [
    { id: "category-food", name: "食費", archived: false },
    { id: "category-daily", name: "日用品", archived: false },
  ],
  defaultAllocation: [],
};

const tags: Tag[] = [
  { id: "tag-regular", householdId: household.id, name: "定期購入", archived: false, revision: 1 },
  { id: "tag-review", householdId: household.id, name: "要確認", archived: false, revision: 1 },
];

function FilterStory({ initialFilters }: { initialFilters: LedgerSearch }) {
  const [filters, setFilters] = useState(initialFilters);

  return (
    <div style={{ width: "min(1080px, 92vw)" }}>
      <LedgerFilters
        household={household}
        filters={filters}
        tags={tags}
        recordCount={filters.recordMode === "purchases" ? "128件" : "12件"}
        onFiltersChange={(updates) => setFilters((current) => ({ ...current, ...updates }))}
        onClearSelection={fn()}
      />
    </div>
  );
}

const meta = {
  title: "Pages/Ledger/Filters",
  component: FilterStory,
  args: { initialFilters: defaultLedgerSearch },
  parameters: { layout: "padded" },
} satisfies Meta<typeof FilterStory>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Purchases: Story = {};

export const FilteredPurchases: Story = {
  args: {
    initialFilters: {
      ...defaultLedgerSearch,
      search: "牛乳",
      fromMonth: "2026-05",
      toMonth: "2026-06",
      categoryFilter: "category-food",
      accountFilter: "account-card",
      tagFilter: "tag-regular",
      minimumAmount: "1000",
    },
  },
};

export const UnmatchedObservations: Story = {
  args: {
    initialFilters: {
      ...defaultLedgerSearch,
      recordMode: "observations",
      observationSourceFilter: "EMAIL",
    },
  },
};

export const UnclassifiedPurchases: Story = {
  args: {
    initialFilters: {
      ...defaultLedgerSearch,
      categoryFilter: "__uncategorized",
      fromMonth: "2026-06",
      toMonth: "2026-06",
    },
  },
};

import type { Meta, StoryObj } from "@storybook/react-vite";

import { fn } from "storybook/test";

import { SettlementPeriodPicker } from "./settlement-period-picker";

import "./settlement-page.css";

const meta = {
  title: "Pages/Settlement/Period picker",
  component: SettlementPeriodPicker,
  args: { period: "2026-06", onChange: fn() },
  parameters: { layout: "centered" },
} satisfies Meta<typeof SettlementPeriodPicker>;

export default meta;
type Story = StoryObj<typeof meta>;

export const SelectedMonth: Story = {};

export const InvalidPeriod: Story = {
  args: { period: "", onChange: fn() },
};

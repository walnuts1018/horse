import type { Meta, StoryObj } from "@storybook/react-vite";

import { fn } from "storybook/test";

import { SettingsTabs } from "./settings-tabs";

const meta = {
  title: "Pages/Settings/Tabs",
  component: SettingsTabs,
  args: { activeTab: "accounts", onSelect: fn() },
  parameters: { layout: "padded" },
} satisfies Meta<typeof SettingsTabs>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Accounts: Story = {};

export const Allocation: Story = { args: { activeTab: "allocation" } };

export const Planning: Story = { args: { activeTab: "planning" } };

import type { Meta, StoryObj } from "@storybook/react-vite";

const galleryStyle = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
  gap: "1rem",
  width: "min(720px, 90vw)",
} as const;

const exampleStyle = {
  display: "grid",
  alignContent: "start",
  gap: "0.75rem",
  padding: "1rem",
  border: "1px solid var(--line)",
  borderRadius: "0.75rem",
  background: "var(--paper)",
} as const;

const meta = {
  title: "Shared UI/Native controls",
  parameters: {
    layout: "centered",
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const Checkboxes: Story = {
  render: () => (
    <div style={galleryStyle}>
      <section style={exampleStyle}>
        <strong>未選択</strong>
        <label>
          <input type="checkbox" />
          食費
        </label>
      </section>
      <section style={exampleStyle}>
        <strong>選択済み</strong>
        <label>
          <input type="checkbox" defaultChecked />
          食費
        </label>
      </section>
      <section style={exampleStyle}>
        <strong>無効</strong>
        <label>
          <input type="checkbox" disabled />
          食費
        </label>
      </section>
    </div>
  ),
};

export const Selects: Story = {
  render: () => (
    <div style={galleryStyle}>
      <section style={exampleStyle}>
        <strong>標準</strong>
        <label>
          科目
          <select defaultValue="food">
            <option value="food">食費</option>
            <option value="daily">日用品</option>
            <option value="transport">交通費</option>
          </select>
        </label>
      </section>
      <section style={exampleStyle}>
        <strong>無効</strong>
        <label>
          科目
          <select defaultValue="food" disabled>
            <option value="food">食費</option>
            <option value="daily">日用品</option>
            <option value="transport">交通費</option>
          </select>
        </label>
      </section>
      <section style={exampleStyle}>
        <strong>複数選択</strong>
        <label>
          科目
          <select defaultValue={["food", "daily"]} multiple size={3}>
            <option value="food">食費</option>
            <option value="daily">日用品</option>
            <option value="transport">交通費</option>
          </select>
        </label>
      </section>
    </div>
  ),
};

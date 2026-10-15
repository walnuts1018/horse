import type { Meta, StoryObj } from "@storybook/react-vite";

import "./imports-page.css";
import styles from "./imports-page.module.css";

const meta = {
  title: "Pages/Imports/Receipt item review",
  parameters: {
    layout: "centered",
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

function ReceiptItemReview({ attention, open }: { attention: boolean; open: boolean }) {
  return (
    <div className="review-fields" style={{ width: "min(640px, 90vw)" }}>
      <details className="receipt-item-review" open={open}>
        <summary className={styles.receiptItemSummary} data-expanded={open}>
          <span>ヨーグルト</span>
          <strong>￥198</strong>
          <small
            className={
              attention ? styles.receiptItemReviewAttention : styles.receiptItemReviewConfirmed
            }
          >
            {attention ? "確認が必要" : "編集"}
          </small>
        </summary>
        {open && (
          <>
            <div>
              <label>
                品名
                <input defaultValue="ヨーグルト" />
              </label>
              <label>
                カテゴリ
                <select defaultValue="food">
                  <option value="food">食料品</option>
                  <option value="daily">日用品</option>
                </select>
              </label>
              <fieldset className={styles.receiptItemReviewState}>
                <legend>確認状況</legend>
                <button className="button button-primary" type="button">
                  一致を確認
                </button>
                <button className="button button-soft" type="button">
                  未解決にする
                </button>
              </fieldset>
            </div>
            <label>
              帳簿に登録する金額
              <input inputMode="numeric" defaultValue="198" />
            </label>
          </>
        )}
      </details>
    </div>
  );
}

export const NeedsReview: Story = {
  render: () => <ReceiptItemReview attention open />,
};

export const Collapsed: Story = {
  render: () => <ReceiptItemReview attention={false} open={false} />,
};

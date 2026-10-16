import type { ParsedPurchaseItemPaste } from "../model/parse-pasted-purchase-items";

import styles from "./purchase-item-paste-review.module.css";

const normalizeAmount = (amount: string) => amount.replace(/[¥,]/g, "");
const isValidAmount = (amount: string) => /^[1-9]\d*$/.test(normalizeAmount(amount));

export function PurchaseItemPasteReview({
  review,
  onClose,
  onRawTextChange,
  onInvalidRowChange,
  onAdd,
}: {
  review: { rawText: string; parsed: ParsedPurchaseItemPaste };
  onClose: () => void;
  onRawTextChange: (text: string) => void;
  onInvalidRowChange: (index: number, field: "name" | "amount", value: string) => void;
  onAdd: () => void;
}) {
  return (
    <section className={styles.review} aria-label="貼り付けた品目">
      <div className={styles.heading}>
        <b>貼り付けた品目</b>
        <button type="button" className="text-button" onClick={onClose}>
          閉じる
        </button>
      </div>
      {review.parsed.items.length > 0 && (
        <ul className={styles.validItems}>
          {review.parsed.items.map((item) => (
            <li key={item.rowNumber}>
              <span>{item.name}</span>
              <span>¥{new Intl.NumberFormat("ja-JP").format(Number(item.amount))}</span>
            </li>
          ))}
        </ul>
      )}
      {review.parsed.invalidSyntax ? (
        <div className={styles.syntaxError}>
          <label>
            <span>CSV</span>
            <textarea
              value={review.rawText}
              onChange={(event) => onRawTextChange(event.target.value)}
              rows={4}
            />
          </label>
          <span role="alert">引用符を確認してください</span>
        </div>
      ) : (
        review.parsed.invalidRows.map((row, index) => (
          <div className={styles.invalidRow} key={row.rowNumber}>
            <span>行{row.rowNumber}</span>
            <label>
              <span>品名</span>
              <input
                value={row.name}
                aria-label={`行${row.rowNumber}の品名`}
                aria-invalid={!row.name.trim()}
                onChange={(event) => onInvalidRowChange(index, "name", event.target.value)}
              />
              {!row.name.trim() && <small>品名を入力</small>}
            </label>
            <label>
              <span>金額</span>
              <input
                inputMode="numeric"
                value={row.amount}
                aria-label={`行${row.rowNumber}の金額`}
                aria-invalid={!isValidAmount(row.amount)}
                onChange={(event) => onInvalidRowChange(index, "amount", event.target.value)}
              />
              {!isValidAmount(row.amount) && <small>正の金額を入力</small>}
            </label>
          </div>
        ))
      )}
      <button
        type="button"
        className="button button-primary"
        disabled={
          review.parsed.invalidSyntax ||
          (!review.parsed.items.length && !review.parsed.invalidRows.length) ||
          review.parsed.invalidRows.some(
            ({ name, amount }) => !name.trim() || !isValidAmount(amount),
          )
        }
        onClick={onAdd}
      >
        品目に追加
      </button>
    </section>
  );
}

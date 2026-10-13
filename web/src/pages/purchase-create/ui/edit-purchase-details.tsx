import type { ReactNode } from "react";

import type { Purchase } from "../../../shared/api";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export function EditPurchaseDetails({
  merchant,
  occurredAt,
  history,
  recentMerchants,
  recentPurchases,
  copiedPurchase,
  classNames,
  onMerchantChange,
  onOccurredAtChange,
  onSelectMerchant,
  onCopyPurchase,
  children,
}: {
  merchant: string;
  occurredAt: string;
  history: Purchase[];
  recentMerchants: string[];
  recentPurchases: Purchase[];
  copiedPurchase: Purchase | null;
  classNames: { details: string; history: string };
  onMerchantChange: (value: string) => void;
  onOccurredAtChange: (value: string) => void;
  onSelectMerchant: (value: string) => void;
  onCopyPurchase: (purchase: Purchase) => void;
  children?: ReactNode;
}) {
  return (
    <>
      <section className={`panel purchase-meta-form ${classNames.details}`}>
        <div className="field-group">
          <label htmlFor="merchant">店名</label>
          <input
            id="merchant"
            placeholder="例 スーパーいちば"
            value={merchant}
            onChange={(event) => onMerchantChange(event.target.value)}
            list="merchant-suggestions"
            autoComplete="off"
          />
          <datalist id="merchant-suggestions">
            {Array.from(new Set(history.map((item) => item.merchant).filter(Boolean)))
              .slice(0, 8)
              .map((name) => (
                <option key={name} value={name} />
              ))}
          </datalist>
        </div>
        <div className="field-group">
          <label htmlFor="occurred">日時</label>
          <input
            id="occurred"
            type="datetime-local"
            value={occurredAt}
            onChange={(event) => onOccurredAtChange(event.target.value)}
          />
        </div>
        {children}
      </section>
      {(recentMerchants.length > 0 || recentPurchases.length > 0) && (
        <section className={`panel ${classNames.history}`} aria-label="購入履歴から入力">
          {recentMerchants.length > 0 && (
            <div className="field-group">
              <span>同じ店でもう一度入力</span>
              <div className="purchase-history-actions">
                {recentMerchants.map((name) => (
                  <button
                    className="text-button"
                    type="button"
                    key={name}
                    onClick={() => onSelectMerchant(name)}
                  >
                    {name}
                  </button>
                ))}
              </div>
            </div>
          )}
          {recentPurchases.length > 0 && (
            <div className="field-group">
              <span>前回の購入をコピー</span>
              <div className="purchase-history-actions">
                {recentPurchases.slice(0, 5).map((purchase) => (
                  <button
                    className="text-button"
                    type="button"
                    key={purchase.id}
                    onClick={() => onCopyPurchase(purchase)}
                  >
                    {purchase.merchant || "店名未入力"} ·{" "}
                    {new Date(purchase.occurredAt).toLocaleDateString("ja-JP")} ·{" "}
                    {yen(purchase.totalYen)}
                  </button>
                ))}
              </div>
              {copiedPurchase && (
                <small role="status">
                  {copiedPurchase.merchant || "店名未入力"}
                  の品目と前回価格をコピーしました。価格、日時、決済元を確認してください。
                </small>
              )}
            </div>
          )}
        </section>
      )}
    </>
  );
}

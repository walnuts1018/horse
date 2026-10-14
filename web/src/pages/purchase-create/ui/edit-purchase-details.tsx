import { useMemo, type ReactNode } from "react";

import type { Purchase } from "../../../shared/api";

import {
  indexRecentPurchasesByMerchant,
  purchaseMerchantKey,
} from "../model/recent-purchase-by-merchant";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const padDatePart = (value: number) => String(value).padStart(2, "0");

export function EditPurchaseDetails({
  merchant,
  occurredAt,
  history,
  recentMerchants,
  recentPurchases,
  copiedPurchase,
  copiedAt,
  occurredAtError,
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
  copiedAt: number | null;
  occurredAtError: boolean;
  classNames: { details: string; history: string };
  onMerchantChange: (value: string) => void;
  onOccurredAtChange: (value: string) => void;
  onSelectMerchant: (value: string) => void;
  onCopyPurchase: (purchase: Purchase) => void;
  children?: ReactNode;
}) {
  const recentPurchasesByMerchant = useMemo(
    () => indexRecentPurchasesByMerchant(recentPurchases),
    [recentPurchases],
  );
  const copiedPurchaseAgeInDays =
    copiedPurchase && copiedAt !== null
      ? Math.floor((copiedAt - new Date(copiedPurchase.occurredAt).getTime()) / 86_400_000)
      : -1;
  const merchantPurchase = recentPurchasesByMerchant.get(purchaseMerchantKey(merchant));
  const setDate = (daysAgo: number) => {
    const date = new Date();
    date.setDate(date.getDate() - daysAgo);
    const time = /T\d{2}:\d{2}$/.test(occurredAt) ? occurredAt.slice(11) : "12:00";
    onOccurredAtChange(
      `${date.getFullYear()}-${padDatePart(date.getMonth() + 1)}-${padDatePart(date.getDate())}T${time}`,
    );
  };

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
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
          </datalist>
          {merchantPurchase && (
            <div className="field-group" aria-label={`${merchantPurchase.merchant}の前回購入`}>
              <span>{merchantPurchase.merchant}の前回購入</span>
              <button
                className="text-button"
                type="button"
                onClick={() => onCopyPurchase(merchantPurchase)}
              >
                買い物をコピー · {new Date(merchantPurchase.occurredAt).toLocaleDateString("ja-JP")}{" "}
                · {yen(merchantPurchase.totalYen)}
              </button>
            </div>
          )}
        </div>
        <div className="field-group">
          <label htmlFor="occurred">日時</label>
          <input
            id="occurred"
            type="datetime-local"
            value={occurredAt}
            aria-invalid={occurredAtError}
            aria-describedby={occurredAtError ? "purchase-occurred-at-error" : undefined}
            onChange={(event) => onOccurredAtChange(event.target.value)}
          />
          {occurredAtError && (
            <small className="entry-error" id="purchase-occurred-at-error" role="alert">
              購入日時を確認してください
            </small>
          )}
          <div className="purchase-history-actions" aria-label="日時のクイック選択">
            <button className="text-button" type="button" onClick={() => setDate(0)}>
              今日
            </button>
            <button className="text-button" type="button" onClick={() => setDate(1)}>
              昨日
            </button>
          </div>
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
                <output>
                  {copiedPurchase.merchant || "店名未入力"}
                  の品目をコピーしました。前回価格は各品目に表示しています。
                  {copiedPurchaseAgeInDays >= 30 && " 前回購入から30日以上経過しています。"}
                </output>
              )}
            </div>
          )}
        </section>
      )}
    </>
  );
}

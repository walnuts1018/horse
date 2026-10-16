import type { Purchase, StatementRefundCorrectionsQuery } from "../../../shared/api";

import { yen, type StatementEntry } from "../model/imports";

type RefundCorrection =
  StatementRefundCorrectionsQuery["response"]["statementRefundCorrections"]["edges"][number];

type RefundTarget = { purchaseId: string; itemId: string };
type RefundCorrectionPage = { after: string | null; hasNextPage: boolean; loading: boolean };

type Props = {
  entry: StatementEntry;
  pending: boolean;
  confirmed: boolean;
  issue?: string;
  target: RefundTarget;
  purchases: Purchase[];
  purchaseState?: { loading: boolean; error: boolean };
  correctionPage: RefundCorrectionPage;
  corrections: RefundCorrection[];
  correctionOpen: boolean;
  correctionError?: string;
  selectedCorrectionId: string;
  onLoadPurchases: () => void;
  onSelectPurchase: (purchaseId: string) => void;
  onSelectItem: (itemId: string) => void;
  onConfirm: (entry: StatementEntry, purchaseId: string, itemId?: string) => void;
  onToggleCorrections: () => void;
  onSelectCorrection: (correctionId: string) => void;
  onLinkCorrection: (entry: StatementEntry, correctionId: string) => void;
  onLoadCorrections: (after?: string) => void;
};

export function RefundEntryEditor({
  entry,
  pending,
  confirmed,
  issue,
  target,
  purchases,
  purchaseState,
  correctionPage,
  corrections,
  correctionOpen,
  correctionError,
  selectedCorrectionId,
  onLoadPurchases,
  onSelectPurchase,
  onSelectItem,
  onConfirm,
  onToggleCorrections,
  onSelectCorrection,
  onLinkCorrection,
  onLoadCorrections,
}: Props) {
  if (issue) return <small>{issue}</small>;
  if (confirmed) return <small>返品登録済み</small>;

  return (
    <>
      <label>
        <span>返品先</span>
        <select
          value={target.purchaseId}
          disabled={pending}
          onFocus={onLoadPurchases}
          onChange={(event) => onSelectPurchase(event.target.value)}
        >
          <option value="">買い物を選択</option>
          {purchases.map((purchase) => (
            <option key={purchase.id} value={purchase.id}>
              {new Date(purchase.occurredAt).toLocaleDateString("ja-JP", {
                timeZone: "Asia/Tokyo",
              })}{" "}
              {purchase.merchant} · {yen(purchase.totalYen)}
            </option>
          ))}
        </select>
      </label>
      {purchaseState?.loading && <small>購入候補を検索しています</small>}
      {purchaseState?.error && (
        <small role="alert">
          購入候補を読み込めませんでした
          <button type="button" className="text-button" onClick={onLoadPurchases}>
            再試行
          </button>
        </small>
      )}
      {purchaseState &&
        !purchaseState.loading &&
        !purchaseState.error &&
        purchases.length === 0 && <small>購入候補が見つかりません</small>}
      {target.purchaseId && (
        <label>
          <span>品目</span>
          <select
            value={target.itemId}
            disabled={pending}
            onChange={(event) => onSelectItem(event.target.value)}
          >
            <option value="">買い物全体</option>
            {purchases
              .find((purchase) => purchase.id === target.purchaseId)
              ?.items.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name} · {yen(item.amountYen)}
                </option>
              ))}
          </select>
        </label>
      )}
      <button
        type="button"
        className="text-button"
        disabled={pending || !target.purchaseId}
        onClick={() => onConfirm(entry, target.purchaseId, target.itemId || undefined)}
      >
        返品を記録
      </button>
      <button
        type="button"
        className="text-button"
        disabled={pending || correctionPage.loading}
        aria-expanded={correctionOpen}
        onClick={onToggleCorrections}
      >
        {correctionPage.loading
          ? "読み込み中"
          : correctionOpen
            ? "候補を閉じる"
            : "既存の返金記録を探す"}
      </button>
      {correctionOpen && (
        <>
          {corrections.length > 0 && (
            <>
              <label>
                <span>既存の返金記録</span>
                <select
                  value={selectedCorrectionId}
                  disabled={pending}
                  onChange={(event) => onSelectCorrection(event.target.value)}
                >
                  <option value="">返金記録を選択</option>
                  {corrections.map(({ node }) => {
                    const label = [
                      `${new Date(node.occurredAt).toLocaleDateString("ja-JP", { timeZone: "Asia/Tokyo" })} ${node.merchant} · ${yen(Number(node.amountYen))}`,
                      node.sourceFilename,
                    ]
                      .filter(Boolean)
                      .join(" · ");
                    return (
                      <option key={node.id} value={node.id}>
                        {label}
                      </option>
                    );
                  })}
                </select>
              </label>
              <button
                type="button"
                className="text-button"
                disabled={pending || !selectedCorrectionId}
                onClick={() => onLinkCorrection(entry, selectedCorrectionId)}
              >
                既存の返金に紐付ける
              </button>
            </>
          )}
          {correctionError && <small role="alert">{correctionError}</small>}
          {correctionPage.hasNextPage && (
            <button
              type="button"
              className="text-button"
              disabled={pending || correctionPage.loading}
              onClick={() => onLoadCorrections(correctionPage.after ?? undefined)}
            >
              {correctionPage.loading ? "読み込み中" : "候補をさらに表示"}
            </button>
          )}
        </>
      )}
    </>
  );
}

import { useState } from "react";

import type { Account, Purchase } from "../../../shared/api";

import {
  statementConfirmationIssue,
  statementSourceReference,
  yen,
  type DocumentReview,
  type StatementEntry,
} from "../model/imports";

export function StatementReview({
  document,
  purchases,
  accounts,
  confirmedStatements,
  pending,
  onConfirm,
  onConfirmRefund,
  onConfirmTransfer,
  onSelectRegion,
}: {
  document: DocumentReview;
  purchases: Purchase[];
  accounts: Account[];
  confirmedStatements: Set<string>;
  pending: boolean;
  onConfirm: (entry: StatementEntry, accountId: string) => void;
  onConfirmRefund: (entry: StatementEntry, purchaseId: string, itemId?: string) => void;
  onConfirmTransfer: (
    entry: StatementEntry,
    statementAccountId: string,
    otherAccountId: string,
  ) => void;
  onSelectRegion: (entry: StatementEntry) => void;
}) {
  const [refundTargets, setRefundTargets] = useState<
    Record<string, { purchaseId: string; itemId: string }>
  >({});
  const [paymentAccounts, setPaymentAccounts] = useState<Record<string, string>>({});
  const [statementAccounts, setStatementAccounts] = useState<
    Record<string, { statementAccountId: string; otherAccountId: string }>
  >({});
  const statements = document.result?.statements;
  if (!statements) return null;

  return (
    <div className="statement-review">
      <div className="panel-heading">
        <div>
          <span className="eyebrow">明細行</span>
          <h3>{statements.length}件</h3>
        </div>
      </div>
      {statements.map((entry) => {
        const reference = statementSourceReference(entry);
        const key = `${document.id}:${reference}`;
        const confirmed = confirmedStatements.has(key);
        const issue = statementConfirmationIssue(entry);
        const refundTarget = refundTargets[key] ?? { purchaseId: "", itemId: "" };
        const paymentAccountId = paymentAccounts[key] ?? "";
        const transferAccounts = statementAccounts[key] ?? {
          statementAccountId: "",
          otherAccountId: "",
        };
        const isRefund = entry.kind === "REFUND";
        const isTransfer = entry.kind === "ACCOUNT_TRANSFER";
        const isCardBillPayment = entry.kind === "CARD_BILL_PAYMENT";
        const isExternalIncome = entry.kind === "EXTERNAL_INCOME";
        return (
          <div
            className="statement-row"
            key={`${entry.row}-${entry.transactionId ?? entry.occurredAt}`}
          >
            <div>
              {entry.region ? (
                <button type="button" className="text-button" onClick={() => onSelectRegion(entry)}>
                  {entry.merchant || "利用先未入力"}
                </button>
              ) : (
                <b>{entry.merchant || "利用先未入力"}</b>
              )}
              <small>
                {entry.occurredAt.slice(0, 10)} ·{" "}
                {isRefund
                  ? "返品・返金"
                  : isTransfer
                    ? "口座間振替"
                    : isCardBillPayment
                      ? "カード引落し"
                      : isExternalIncome
                        ? "外部入金"
                        : entry.kind === "CARD_PURCHASE"
                          ? "カード利用"
                          : entry.kind}
                {entry.cardLast4 ? ` · •••• ${entry.cardLast4}` : ""}
              </small>
            </div>
            <div className="statement-row-action">
              <strong>{yen(entry.amount)}</strong>
              {isRefund ? (
                issue ? (
                  <small>{issue}</small>
                ) : confirmed ? (
                  <small>返品登録済み</small>
                ) : (
                  <>
                    <label>
                      <span>返品先</span>
                      <select
                        value={refundTarget.purchaseId}
                        disabled={pending}
                        onChange={(event) =>
                          setRefundTargets((current) => ({
                            ...current,
                            [key]: { purchaseId: event.target.value, itemId: "" },
                          }))
                        }
                      >
                        <option value="">買い物を選択</option>
                        {purchases
                          .filter((purchase) => purchase.status !== "DRAFT")
                          .map((purchase) => (
                            <option key={purchase.id} value={purchase.id}>
                              {new Date(purchase.occurredAt).toLocaleDateString("ja-JP", {
                                timeZone: "Asia/Tokyo",
                              })}{" "}
                              {purchase.merchant} · {yen(purchase.totalYen)}
                            </option>
                          ))}
                      </select>
                    </label>
                    {refundTarget.purchaseId && (
                      <label>
                        <span>品目</span>
                        <select
                          value={refundTarget.itemId}
                          disabled={pending}
                          onChange={(event) =>
                            setRefundTargets((current) => ({
                              ...current,
                              [key]: { ...refundTarget, itemId: event.target.value },
                            }))
                          }
                        >
                          <option value="">買い物全体</option>
                          {purchases
                            .find((purchase) => purchase.id === refundTarget.purchaseId)
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
                      disabled={pending || !refundTarget.purchaseId}
                      onClick={() =>
                        onConfirmRefund(
                          entry,
                          refundTarget.purchaseId,
                          refundTarget.itemId || undefined,
                        )
                      }
                    >
                      返品を記録
                    </button>
                  </>
                )
              ) : isTransfer || isCardBillPayment ? (
                issue ? (
                  <small>{issue}</small>
                ) : confirmed ? (
                  <small>{isCardBillPayment ? "カード引落し登録済み" : "振替登録済み"}</small>
                ) : (
                  <>
                    <label>
                      <span>
                        {isCardBillPayment
                          ? "引落口座"
                          : entry.amount < 0
                            ? "振替元の明細口座"
                            : "振替先の明細口座"}
                      </span>
                      <select
                        value={transferAccounts.statementAccountId}
                        disabled={pending}
                        onChange={(event) =>
                          setStatementAccounts((current) => ({
                            ...current,
                            [key]: { statementAccountId: event.target.value, otherAccountId: "" },
                          }))
                        }
                      >
                        <option value="">口座を選択</option>
                        {accounts
                          .filter((account) =>
                            isCardBillPayment ? account.kind === "BANK" : account.kind !== "CARD",
                          )
                          .map((account) => (
                            <option key={account.id} value={account.id}>
                              {account.name}
                            </option>
                          ))}
                      </select>
                    </label>
                    <label>
                      <span>
                        {isCardBillPayment
                          ? "引落対象のカード"
                          : entry.amount < 0
                            ? "振替先"
                            : "振替元"}
                      </span>
                      <select
                        value={transferAccounts.otherAccountId}
                        disabled={pending || !transferAccounts.statementAccountId}
                        onChange={(event) =>
                          setStatementAccounts((current) => ({
                            ...current,
                            [key]: { ...transferAccounts, otherAccountId: event.target.value },
                          }))
                        }
                      >
                        <option value="">口座を選択</option>
                        {accounts
                          .filter(
                            (account) =>
                              (isCardBillPayment
                                ? account.kind === "CARD"
                                : account.kind !== "CARD") &&
                              account.id !== transferAccounts.statementAccountId,
                          )
                          .map((account) => (
                            <option key={account.id} value={account.id}>
                              {account.name}
                            </option>
                          ))}
                      </select>
                    </label>
                    <button
                      type="button"
                      className="text-button"
                      disabled={
                        pending ||
                        !transferAccounts.statementAccountId ||
                        !transferAccounts.otherAccountId
                      }
                      onClick={() =>
                        onConfirmTransfer(
                          entry,
                          transferAccounts.statementAccountId,
                          transferAccounts.otherAccountId,
                        )
                      }
                    >
                      {isCardBillPayment ? "カード引落しを記録" : "振替を記録"}
                    </button>
                  </>
                )
              ) : (
                (entry.kind === "CARD_PURCHASE" || isExternalIncome) &&
                (issue ? (
                  <small>{issue}</small>
                ) : confirmed ? (
                  <small>{isExternalIncome ? "入金登録済み" : "追加済み"}</small>
                ) : (
                  <>
                    <label>
                      <span>{isExternalIncome ? "入金先の銀行口座" : "決済口座"}</span>
                      <select
                        value={paymentAccountId}
                        disabled={pending}
                        onChange={(event) =>
                          setPaymentAccounts((current) => ({
                            ...current,
                            [key]: event.target.value,
                          }))
                        }
                      >
                        <option value="">口座を選択</option>
                        {accounts
                          .filter((account) => !isExternalIncome || account.kind === "BANK")
                          .map((account) => (
                            <option key={account.id} value={account.id}>
                              {account.name}
                            </option>
                          ))}
                      </select>
                    </label>
                    <button
                      type="button"
                      className="text-button"
                      disabled={pending || !paymentAccountId}
                      onClick={() => onConfirm(entry, paymentAccountId)}
                    >
                      {isExternalIncome ? "入金を記録" : "購入に追加"}
                    </button>
                  </>
                ))
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

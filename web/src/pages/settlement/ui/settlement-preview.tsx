import { ArrowLeftRight, Wallet } from "lucide-react";

import type { Household, Settlement } from "../../../shared/api";
import type { FundingIntent } from "../model/pending-ledger-mutations";

import { formatSettlementYen, settlementPeriodLabel } from "../model/settlement-view";

export function SettlementPreview({
  household,
  settlement,
  period,
  totalDue,
  pending,
  pendingFunding,
  fundingSourceAccounts,
  fundingAmounts,
  fundingDestinationAccountId,
  savedFundingSourceAccounts,
  savedFundingDestinationAccountId,
  onFundingSourceChange,
  onFundingAmountChange,
  onFundingDestinationChange,
  onRecordFunding,
  onRestoreFunding,
}: {
  household: Household;
  settlement: Settlement | null;
  period: string;
  totalDue: number;
  pending: boolean;
  pendingFunding: Partial<Record<string, FundingIntent>>;
  fundingSourceAccounts: Record<string, string>;
  fundingAmounts: Partial<Record<string, string>>;
  fundingDestinationAccountId: string;
  savedFundingSourceAccounts: Record<string, string>;
  savedFundingDestinationAccountId: string;
  onFundingSourceChange: (memberId: string, accountId: string) => void;
  onFundingAmountChange: (memberId: string, amount: string) => void;
  onFundingDestinationChange: (accountId: string) => void;
  onRecordFunding: (memberId: string) => void;
  onRestoreFunding: (memberId: string) => void;
}) {
  const sharedPoolIds = new Set(
    household.pools
      .filter(
        (pool) => pool.__typename === "SharedPool" || pool.__typename === "UnconfiguredSharedPool",
      )
      .map((pool) => pool.id),
  );

  return (
    <>
      <section className="settlement-hero">
        <div>
          <span>次回の入金</span>
          <h2>{formatSettlementYen(totalDue)}</h2>
          <small>
            {settlement?.__typename === "ClosedSettlement"
              ? "この期間は締め済み"
              : `${settlementPeriodLabel(period)}分`}
          </small>
        </div>
        <div className="settlement-mark">
          <ArrowLeftRight size={32} />
        </div>
      </section>
      <section className="panel settlement-panel">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">メンバー別</span>
            <h2>入金予定</h2>
          </div>
          <span className="period-chip">
            {settlement?.__typename === "ClosedSettlement" ? "締め済み" : "未締め"}
          </span>
        </div>
        <details className="settlement-disclosure">
          <summary className="settlement-disclosure-summary">
            共通入金先:{" "}
            {household.accounts.find((account) => account.id === fundingDestinationAccountId)
              ?.name ?? "未指定"}
          </summary>
          <label className="settlement-account-select">
            <span>共通入金先</span>
            <select
              value={fundingDestinationAccountId}
              disabled={pending || Object.keys(pendingFunding).length > 0}
              onChange={(event) => onFundingDestinationChange(event.target.value)}
            >
              <option value="">未指定</option>
              {household.accounts
                .filter(
                  (account) =>
                    account.poolId != null &&
                    sharedPoolIds.has(account.poolId) &&
                    account.kind !== "CARD",
                )
                .map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.name}
                  </option>
                ))}
            </select>
          </label>
        </details>
        {!settlement ? (
          <div className="skeleton-list">
            <i />
            <i />
          </div>
        ) : settlement.lines.length === 0 ? (
          <div className="empty-state compact">
            <Wallet />
            <span>精算する入金はありません</span>
          </div>
        ) : (
          settlement.lines.map((line) => {
            const pendingIntent = pendingFunding[line.memberId];
            return (
              <div className="settlement-line" key={line.memberId}>
                <div className="profile-avatar">{line.memberName.slice(0, 1)}</div>
                <div className="settlement-person">
                  <b>{line.memberName}</b>
                  <dl className="settlement-breakdown">
                    <div>
                      <dt>今月予定</dt>
                      <dd>{formatSettlementYen(line.scheduledYen)}</dd>
                    </div>
                    <div>
                      <dt>前月残高</dt>
                      <dd>{formatSettlementYen(line.priorBalanceYen)}</dd>
                    </div>
                    <div>
                      <dt>月次調整</dt>
                      <dd>{formatSettlementYen(line.monthlyAdjustmentYen)}</dd>
                    </div>
                    <div>
                      <dt>入金済み</dt>
                      <dd>{formatSettlementYen(line.paidYen)}</dd>
                    </div>
                    <div>
                      <dt>立替充当</dt>
                      <dd>{formatSettlementYen(line.advanceCreditYen)}</dd>
                    </div>
                  </dl>
                </div>
                <div className="settlement-amount">
                  <b className={line.amountDueYen < 0 ? "settlement-credit" : undefined}>
                    {formatSettlementYen(line.amountDueYen)}
                  </b>
                  <small>次回精算残高</small>
                  <details className="settlement-disclosure">
                    <summary className="settlement-disclosure-summary">
                      {household.accounts.find(
                        (account) => account.id === fundingSourceAccounts[line.memberId],
                      )?.name ?? "口座外"}
                      {" → "}
                      {household.accounts.find(
                        (account) => account.id === fundingDestinationAccountId,
                      )?.name ?? "未指定"}
                    </summary>
                    <label className="settlement-account-select">
                      <span>拠出元</span>
                      <select
                        value={fundingSourceAccounts[line.memberId] ?? ""}
                        disabled={pending || Boolean(pendingIntent)}
                        onChange={(event) =>
                          onFundingSourceChange(line.memberId, event.target.value)
                        }
                      >
                        <option value="">口座外</option>
                        {household.accounts
                          .filter((account) => {
                            const pool = household.pools.find(
                              (candidate) => candidate.id === account.poolId,
                            );
                            return (
                              pool?.__typename === "PersonalPool" &&
                              pool.ownerMemberId === line.memberId
                            );
                          })
                          .map((account) => (
                            <option key={account.id} value={account.id}>
                              {account.name}
                            </option>
                          ))}
                      </select>
                    </label>
                  </details>
                  {line.amountDueYen > 0 ? (
                    <label className="settlement-account-select">
                      <span>今回の入金額</span>
                      <input
                        type="number"
                        inputMode="numeric"
                        min={1}
                        max={1_000_000_000_000}
                        step={1}
                        value={fundingAmounts[line.memberId] ?? String(line.amountDueYen)}
                        disabled={pending || Boolean(pendingIntent)}
                        aria-label={`${line.memberName}の今回の入金額`}
                        onChange={(event) =>
                          onFundingAmountChange(line.memberId, event.target.value)
                        }
                      />
                    </label>
                  ) : (
                    <p className="settlement-no-funding">
                      {line.amountDueYen < 0
                        ? `過払い・控除 ${formatSettlementYen(Math.abs(line.amountDueYen))}`
                        : "今回の入金はありません"}
                    </p>
                  )}
                  <button
                    className="text-button"
                    type="button"
                    disabled={pending || (line.amountDueYen <= 0 && !pendingIntent)}
                    onClick={() => onRecordFunding(line.memberId)}
                  >
                    {pendingIntent
                      ? `入金を再試行 (${formatSettlementYen(pendingIntent.input.amountYen)})`
                      : (fundingAmounts[line.memberId] ?? String(line.amountDueYen)) ===
                          String(line.amountDueYen)
                        ? "予定額を入金"
                        : "変更額を入金"}
                  </button>
                  {((fundingSourceAccounts[line.memberId] ?? "") !==
                    (savedFundingSourceAccounts[line.memberId] ?? "") ||
                    fundingDestinationAccountId !== savedFundingDestinationAccountId ||
                    (fundingAmounts[line.memberId] != null &&
                      fundingAmounts[line.memberId] !== String(line.amountDueYen))) && (
                    <button
                      className="text-button"
                      type="button"
                      disabled={pending}
                      onClick={() => onRestoreFunding(line.memberId)}
                    >
                      入力を戻す
                    </button>
                  )}
                </div>
              </div>
            );
          })
        )}
      </section>
    </>
  );
}

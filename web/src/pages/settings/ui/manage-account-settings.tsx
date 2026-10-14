import { Check, ChevronRight, Plus, Wallet } from "lucide-react";

import {
  isPersonalPool,
  isSharedPool,
  type AccountBalance,
  type Household,
} from "../../../shared/api";
import { isOneOf } from "../../../shared/lib/guard";
import { useAccountSettings } from "../model/account-settings";

import styles from "./settings-page.module.css";

const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

export function ManageAccountSettings({
  household,
  balances,
  hidden,
  onRefresh,
  onRefreshBalances,
  onNotice,
  onFormClean,
  pending,
}: {
  household: Household;
  balances: AccountBalance[];
  hidden: boolean;
  onRefresh: () => Promise<void>;
  onRefreshBalances: () => Promise<void>;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormClean: (formID: string) => void;
  pending: boolean;
}) {
  const model = useAccountSettings({
    household,
    onRefresh,
    onRefreshBalances,
    onNotice,
    onFormClean,
  });

  return (
    <>
      <section
        className={`panel ${styles.settingsSection}`}
        hidden={hidden}
        id="pool-settings"
        style={{ scrollMarginTop: 80 }}
      >
        <div className={`panel-heading ${styles.panelHeading}`}>
          <div>
            <span className="eyebrow">負担先</span>
            <h2>マネープール</h2>
          </div>
          <span className="count-badge">{household.pools.length}</span>
        </div>
        <div className={styles.settingsRows}>
          {household.pools.map((pool) => (
            <div className={styles.poolFundingCard} key={pool.id}>
              <div className={styles.settingRow}>
                <div className={`pool-avatar ${styles.poolAvatar}`}>{pool.name.slice(0, 1)}</div>
                <div>
                  <b>{pool.name}</b>
                  <small>
                    {isSharedPool(pool) ? "共用" : "個人"} · 品目負担の標準比{" "}
                    {pool.defaultShareWeight}
                  </small>
                </div>
                {pool.__typename === "UnconfiguredPersonalPool" ||
                pool.__typename === "UnconfiguredSharedPool" ? (
                  <span className="period-chip">設定が必要</span>
                ) : null}
                <ChevronRight size={16} />
              </div>
              <form
                className={styles.poolFundingForm}
                data-unsaved-id={`pool-funding:${pool.id}`}
                onSubmit={(event) => void model.saveFundingRule(event, pool.id)}
              >
                {isPersonalPool(pool) ? (
                  <label>
                    所有者
                    <select
                      required
                      value={model.fundingDrafts[pool.id].ownerMemberId}
                      onChange={(event) =>
                        model.setFundingDrafts((current) => ({
                          ...current,
                          [pool.id]: { ...current[pool.id], ownerMemberId: event.target.value },
                        }))
                      }
                    >
                      <option value="">メンバーを選択</option>
                      {household.members.map((member) => (
                        <option key={member.id} value={member.id}>
                          {member.displayName}
                        </option>
                      ))}
                    </select>
                  </label>
                ) : (
                  <>
                    <label>
                      精算方法
                      <select
                        value={model.fundingDrafts[pool.id].policy}
                        onChange={(event) => {
                          const policy = event.target.value;
                          if (!isOneOf(policy, ["BALANCE_TO_STANDARD", "REIMBURSE_PERSONAL"]))
                            return;
                          model.setFundingDrafts((current) => ({
                            ...current,
                            [pool.id]: {
                              ...current[pool.id],
                              policy,
                            },
                          }));
                        }}
                      >
                        <option value="BALANCE_TO_STANDARD">負担割合の平準化</option>
                        <option value="REIMBURSE_PERSONAL">予算外支出の補填</option>
                      </select>
                    </label>
                    <div className={styles.poolFundingShares}>
                      {household.members.map((member) => (
                        <label key={member.id}>
                          <span>{member.displayName}</span>
                          <div className="number-field">
                            <input
                              inputMode="numeric"
                              value={model.fundingDrafts[pool.id]?.weights[member.id] ?? ""}
                              onChange={(event) =>
                                model.setFundingDrafts((current) => ({
                                  ...current,
                                  [pool.id]: {
                                    ...current[pool.id],
                                    weights: {
                                      ...current[pool.id].weights,
                                      [member.id]: event.target.value.replace(/\D/g, ""),
                                    },
                                  },
                                }))
                              }
                            />
                            <small>比</small>
                          </div>
                        </label>
                      ))}
                    </div>
                  </>
                )}
                <button type="submit" className="button button-soft" disabled={model.pending}>
                  <Check size={15} />
                  精算設定を保存
                </button>
              </form>
            </div>
          ))}
        </div>
        <form className="inline-create" data-unsaved-id="create-pool" onSubmit={model.addPool}>
          <input
            value={model.poolName}
            onChange={(event) => model.setPoolName(event.target.value)}
            placeholder="プール名"
            required
          />
          <select
            value={model.poolKind}
            onChange={(event) => {
              const kind = event.target.value;
              if (isOneOf(kind, ["PERSONAL", "SHARED"])) model.setPoolKind(kind);
            }}
          >
            <option value="PERSONAL">個人</option>
            <option value="SHARED">共用</option>
          </select>
          <button type="submit" className="button button-soft" disabled={model.pending}>
            <Plus size={15} />
            追加
          </button>
        </form>
      </section>
      <section
        className={`panel ${styles.settingsSection}`}
        hidden={hidden}
        id="payment-source-settings"
        style={{ scrollMarginTop: 80 }}
      >
        <div className={`panel-heading ${styles.panelHeading}`}>
          <div>
            <span className="eyebrow">決済に使う口座・カード</span>
            <h2>決済元</h2>
          </div>
          <span className="count-badge">{household.accounts.length}</span>
        </div>
        <div className={styles.settingsRows}>
          {household.accounts.map((account) => (
            <div className={styles.settingRow} key={account.id}>
              <div className={styles.accountAvatar}>
                <Wallet size={16} />
              </div>
              <div>
                <b>{account.name}</b>
                <small>
                  {household.pools.find((pool) => pool.id === account.poolId)?.name ?? "個人"} ·{" "}
                  {(
                    { BANK: "銀行", CASH: "現金", CARD: "カード", OTHER: "その他" } as Record<
                      string,
                      string
                    >
                  )[account.kind] ?? account.kind}{" "}
                  · 残高{" "}
                  {yen(
                    balances.find((balance) => balance.accountId === account.id)?.balanceYen ?? 0,
                  )}
                </small>
              </div>
              <ChevronRight size={16} />
            </div>
          ))}
        </div>
        <form
          className="inline-create"
          data-unsaved-id="create-account"
          onSubmit={model.addAccount}
        >
          <input
            value={model.accountName}
            onChange={(event) => model.setAccountName(event.target.value)}
            placeholder="口座・カード名"
            required
          />
          <select
            value={model.accountKind}
            onChange={(event) => {
              const kind = event.target.value;
              if (isOneOf(kind, ["BANK", "CASH", "CARD", "OTHER"])) model.setAccountKind(kind);
            }}
          >
            <option value="BANK">銀行</option>
            <option value="CASH">現金</option>
            <option value="CARD">カード</option>
            <option value="OTHER">その他</option>
          </select>
          <select
            value={model.accountPool}
            onChange={(event) => model.setAccountPool(event.target.value)}
          >
            <option value="">紐づくプール</option>
            {household.pools.map((pool) => (
              <option key={pool.id} value={pool.id}>
                {pool.name}
              </option>
            ))}
          </select>
          <input
            aria-label="初期残高"
            type="number"
            step="1"
            value={model.openingBalanceYen}
            onChange={(event) => model.setOpeningBalanceYen(event.target.value)}
            placeholder="初期残高"
          />
          {model.openingBalanceYen && (
            <label>
              基準日
              <input
                aria-label="初期残高の基準日"
                type="date"
                value={model.openingBalanceAt}
                onChange={(event) => model.setOpeningBalanceAt(event.target.value)}
                required
              />
            </label>
          )}
          <button type="submit" className="button button-soft" disabled={model.pending}>
            <Plus size={15} />
            追加
          </button>
        </form>
      </section>
    </>
  );
}

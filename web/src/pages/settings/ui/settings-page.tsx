import { useEffect, useState, type FormEvent } from "react";
import { Activity, Check, ChevronRight, Plus, Wallet } from "lucide-react";
import {
  createAccountMutation,
  createCategoryMutation,
  createPoolMutation,
  allocationRulesQuery,
  setAllocationRuleMutation,
  setCategoryArchivedMutation,
  readGraphql,
  setDefaultAllocationMutation,
  type AllocationRule,
  type Household,
} from "../../../shared/api";

export function SettingsPage({
  household,
  onRefresh,
  onNotice,
}: {
  household: Household;
  onRefresh: () => Promise<void>;
  onNotice: (message: string) => void;
}) {
  const [poolName, setPoolName] = useState("");
  const [poolKind, setPoolKind] = useState("PERSONAL");
  const [accountName, setAccountName] = useState("");
  const [accountPool, setAccountPool] = useState("");
  const [categoryName, setCategoryName] = useState("");
  const [allocationRules, setAllocationRules] = useState<AllocationRule[]>([]);
  const [ruleScope, setRuleScope] = useState<"CATEGORY" | "MERCHANT">("CATEGORY");
  const [ruleCategoryId, setRuleCategoryId] = useState("");
  const [ruleMerchant, setRuleMerchant] = useState("");
  const [ruleWeights, setRuleWeights] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      household.pools.map((pool) => [pool.id, String(pool.defaultShareWeight || 1)]),
    ),
  );
  const [shareWeights, setShareWeights] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      household.pools.map((pool) => [pool.id, String(pool.defaultShareWeight || 1)]),
    ),
  );
  const [pending, setPending] = useState(false);
  const refreshRules = async () => {
    const result = await readGraphql<{ allocationRules: AllocationRule[] }>(allocationRulesQuery, {
      householdId: household.id,
    });
    setAllocationRules(result.allocationRules);
  };
  useEffect(() => {
    void refreshRules().catch((reason) => onNotice((reason as Error).message));
  }, [household.id]);
  const saveMutation = async (
    mutation: Parameters<typeof readGraphql>[0],
    input: Record<string, unknown>,
  ) => {
    setPending(true);
    try {
      await readGraphql<Record<string, unknown>>(mutation, { input });
      await onRefresh();
      onNotice("設定を保存しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const addPool = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(createPoolMutation, {
      householdId: household.id,
      name: poolName.trim(),
      kind: poolKind,
    }).then(() => setPoolName(""));
  };
  const addAccount = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(createAccountMutation, {
      householdId: household.id,
      name: accountName.trim(),
      kind: "BANK",
      poolId: accountPool || null,
    }).then(() => setAccountName(""));
  };
  const addCategory = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(createCategoryMutation, {
      householdId: household.id,
      name: categoryName.trim(),
    }).then(() => setCategoryName(""));
  };
  const saveAllocation = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(setDefaultAllocationMutation, {
      householdId: household.id,
      allocations: household.pools
        .map((pool) => ({
          poolId: pool.id,
          weight: Math.max(0, Number(shareWeights[pool.id]) || 0),
        }))
        .filter((item) => item.weight > 0),
    });
  };
  const archiveCategory = async (categoryId: string, archived: boolean) => {
    setPending(true);
    try {
      await readGraphql(setCategoryArchivedMutation, { categoryId, archived });
      await onRefresh();
      onNotice(archived ? "カテゴリを非表示にしました" : "カテゴリを再表示しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const saveRule = async (event: FormEvent) => {
    event.preventDefault();
    const allocations = household.pools
      .map((pool) => ({ poolId: pool.id, weight: Math.max(0, Number(ruleWeights[pool.id]) || 0) }))
      .filter((allocation) => allocation.weight > 0);
    if (
      !allocations.length ||
      (ruleScope === "CATEGORY" && !ruleCategoryId) ||
      (ruleScope === "MERCHANT" && !ruleMerchant.trim())
    )
      return;
    setPending(true);
    try {
      await readGraphql(setAllocationRuleMutation, {
        input: {
          householdId: household.id,
          ...(ruleScope === "CATEGORY"
            ? { categoryId: ruleCategoryId }
            : { merchant: ruleMerchant.trim() }),
          allocations,
        },
      });
      await refreshRules();
      onNotice("負担ルールを保存しました");
      setRuleMerchant("");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">共有と分類</span>
          <h1>設定</h1>
          <p>{household.name}の口座、決済元、カテゴリ</p>
        </div>
      </div>
      <div className="settings-layout">
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">負担先</span>
              <h2>マネープール</h2>
            </div>
            <span className="count-badge">{household.pools.length}</span>
          </div>
          <div className="settings-rows">
            {household.pools.map((pool) => (
              <div className="setting-row" key={pool.id}>
                <div className="pool-avatar">{pool.name.slice(0, 1)}</div>
                <div>
                  <b>{pool.name}</b>
                  <small>
                    {pool.kind === "SHARED" ? "共用" : "個人"} · 標準比率 {pool.defaultShareWeight}
                  </small>
                </div>
                <ChevronRight size={16} />
              </div>
            ))}
          </div>
          <form className="inline-create" onSubmit={addPool}>
            <input
              value={poolName}
              onChange={(e) => setPoolName(e.target.value)}
              placeholder="プール名"
              required
            />
            <select value={poolKind} onChange={(e) => setPoolKind(e.target.value)}>
              <option value="PERSONAL">個人</option>
              <option value="SHARED">共用</option>
            </select>
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
        </section>
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">決済に使う口座・カード</span>
              <h2>決済元</h2>
            </div>
            <span className="count-badge">{household.accounts.length}</span>
          </div>
          <div className="settings-rows">
            {household.accounts.map((account) => (
              <div className="setting-row" key={account.id}>
                <div className="account-avatar">
                  <Wallet size={16} />
                </div>
                <div>
                  <b>{account.name}</b>
                  <small>
                    {household.pools.find((pool) => pool.id === account.poolId)?.name ?? "個人"} ·{" "}
                    {account.kind}
                  </small>
                </div>
                <ChevronRight size={16} />
              </div>
            ))}
          </div>
          <form className="inline-create" onSubmit={addAccount}>
            <input
              value={accountName}
              onChange={(e) => setAccountName(e.target.value)}
              placeholder="口座・カード名"
              required
            />
            <select value={accountPool} onChange={(e) => setAccountPool(e.target.value)}>
              <option value="">紐づくプール</option>
              {household.pools.map((pool) => (
                <option key={pool.id} value={pool.id}>
                  {pool.name}
                </option>
              ))}
            </select>
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
        </section>
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">品目の分類</span>
              <h2>カテゴリ</h2>
            </div>
            <span className="count-badge">
              {household.categories.filter((category) => !category.archived).length}
            </span>
          </div>
          <div className="category-management">
            {household.categories.map((category, index) => (
              <div className="category-management-row" key={category.id}>
                <span
                  className={`category-tag swatch-bg-${index % 6} ${category.archived ? "category-archived" : ""}`}
                >
                  {category.name}
                </span>
                <button
                  className="text-button"
                  disabled={pending}
                  onClick={() => void archiveCategory(category.id, !category.archived)}
                >
                  {category.archived ? "再表示" : "非表示"}
                </button>
              </div>
            ))}
          </div>
          <form className="inline-create" onSubmit={addCategory}>
            <input
              value={categoryName}
              onChange={(e) => setCategoryName(e.target.value)}
              placeholder="カテゴリ名"
              required
            />
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
        </section>
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">買い物の初期値</span>
              <h2>標準の負担割合</h2>
            </div>
            <span className="count-badge">
              <Activity size={14} />
            </span>
          </div>
          <form onSubmit={saveAllocation}>
            <div className="allocation-settings">
              {household.pools.map((pool) => (
                <label key={pool.id}>
                  <span>{pool.name}</span>
                  <div className="number-field">
                    <input
                      inputMode="numeric"
                      value={shareWeights[pool.id] ?? ""}
                      onChange={(e) =>
                        setShareWeights((current) => ({
                          ...current,
                          [pool.id]: e.target.value.replace(/\D/g, ""),
                        }))
                      }
                    />
                    <small>比</small>
                  </div>
                </label>
              ))}
            </div>
            <div className="settings-save">
              <span>変更後の買い物から適用</span>
              <button className="button button-primary" disabled={pending}>
                <Check size={16} />
                保存
              </button>
            </div>
          </form>
        </section>
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">自動負担</span>
              <h2>優先ルール</h2>
            </div>
            <span className="count-badge">{allocationRules.length}</span>
          </div>
          <div className="settings-rows">
            {allocationRules.map((rule) => (
              <div className="setting-row" key={rule.id}>
                <div className="pool-avatar">{rule.scope === "CATEGORY" ? "分" : "店"}</div>
                <div>
                  <b>
                    {rule.scope === "CATEGORY"
                      ? (household.categories.find((category) => category.id === rule.categoryId)
                          ?.name ?? "カテゴリ")
                      : rule.merchant}
                  </b>
                  <small>
                    {rule.allocations
                      .map(
                        (allocation) =>
                          `${household.pools.find((pool) => pool.id === allocation.poolId)?.name ?? "プール"} ${allocation.weight}`,
                      )
                      .join(" / ")}
                  </small>
                </div>
              </div>
            ))}
          </div>
          <form className="allocation-rule-form" onSubmit={saveRule}>
            <select
              value={ruleScope}
              onChange={(event) => setRuleScope(event.target.value as "CATEGORY" | "MERCHANT")}
            >
              <option value="CATEGORY">カテゴリ</option>
              <option value="MERCHANT">店名</option>
            </select>
            {ruleScope === "CATEGORY" ? (
              <select
                value={ruleCategoryId}
                onChange={(event) => setRuleCategoryId(event.target.value)}
                required
              >
                <option value="">カテゴリを選択</option>
                {household.categories
                  .filter((category) => !category.archived)
                  .map((category) => (
                    <option key={category.id} value={category.id}>
                      {category.name}
                    </option>
                  ))}
              </select>
            ) : (
              <input
                value={ruleMerchant}
                onChange={(event) => setRuleMerchant(event.target.value)}
                placeholder="店名"
                required
              />
            )}
            <div className="allocation-settings">
              {household.pools.map((pool) => (
                <label key={pool.id}>
                  <span>{pool.name}</span>
                  <div className="number-field">
                    <input
                      inputMode="numeric"
                      value={ruleWeights[pool.id] ?? ""}
                      onChange={(event) =>
                        setRuleWeights((current) => ({
                          ...current,
                          [pool.id]: event.target.value.replace(/\D/g, ""),
                        }))
                      }
                    />
                    <small>比</small>
                  </div>
                </label>
              ))}
            </div>
            <button className="button button-primary" disabled={pending}>
              <Check size={16} />
              ルールを保存
            </button>
          </form>
        </section>
        <section className="panel settings-section member-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">共同編集</span>
              <h2>メンバー</h2>
            </div>
          </div>
          {household.members.map((member) => (
            <div className="setting-row" key={member.id}>
              <div className="profile-avatar">{member.displayName.slice(0, 1)}</div>
              <div>
                <b>{member.displayName}</b>
                <small>{member.role === "OWNER" ? "オーナー" : "メンバー"}</small>
              </div>
            </div>
          ))}
        </section>
      </div>
    </>
  );
}

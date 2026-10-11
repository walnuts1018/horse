import { useEffect, useState, type FormEvent } from "react";
import { Activity, Check, ChevronRight, Plus, Wallet } from "lucide-react";
import {
  createAccountMutation,
  createCategoryMutation,
  createPoolMutation,
  allocationRulesQuery,
  setAllocationRuleMutation,
  setCategoryArchivedMutation,
  createHouseholdInvitationMutation,
  householdInvitationsQuery,
  revokeHouseholdInvitationMutation,
  productCatalogQuery,
  createProductConceptMutation,
  createProductVariantMutation,
  setProductAliasMutation,
  readGraphql,
  setDefaultAllocationMutation,
  setPoolFundingRuleMutation,
  type SharedPoolSettlementPolicy,
  type AllocationRule,
  type HouseholdInvitation,
  type ProductConcept,
  type Household,
} from "../../../shared/api";

export function SettingsPage({
  household,
  onRefresh,
  onNotice,
  isOwner,
}: {
  household: Household;
  onRefresh: () => Promise<void>;
  onNotice: (message: string) => void;
  isOwner: boolean;
}) {
  const [poolName, setPoolName] = useState("");
  const [poolKind, setPoolKind] = useState("PERSONAL");
  const [accountName, setAccountName] = useState("");
  const [accountPool, setAccountPool] = useState("");
  const [categoryName, setCategoryName] = useState("");
  const [allocationRules, setAllocationRules] = useState<AllocationRule[]>([]);
  const [ruleScope, setRuleScope] = useState<
    "CATEGORY" | "MERCHANT" | "PRODUCT" | "PRODUCT_VARIANT"
  >("CATEGORY");
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
  const [fundingDrafts, setFundingDrafts] = useState<
    Record<
      string,
      {
        ownerMemberId: string;
        policy: SharedPoolSettlementPolicy;
        weights: Record<string, string>;
      }
    >
  >({});
  const [pending, setPending] = useState(false);
  const [catalog, setCatalog] = useState<ProductConcept[]>([]);
  const [invitations, setInvitations] = useState<HouseholdInvitation[]>([]);
  const [invitationLink, setInvitationLink] = useState("");
  const [issuedInvitationId, setIssuedInvitationId] = useState("");
  const [expiresInDays, setExpiresInDays] = useState("7");
  const [conceptName, setConceptName] = useState("");
  const [conceptCategoryId, setConceptCategoryId] = useState("");
  const [variantConceptId, setVariantConceptId] = useState("");
  const [variantName, setVariantName] = useState("");
  const [variantBrand, setVariantBrand] = useState("");
  const [variantPackageSize, setVariantPackageSize] = useState("");
  const [aliasVariantId, setAliasVariantId] = useState("");
  const [aliasMerchant, setAliasMerchant] = useState("");
  const [aliasName, setAliasName] = useState("");
  const [ruleProductId, setRuleProductId] = useState("");
  const [ruleVariantId, setRuleVariantId] = useState("");
  const refreshRules = async () => {
    const result = await readGraphql<{ allocationRules: AllocationRule[] }>(allocationRulesQuery, {
      householdId: household.id,
    });
    setAllocationRules(result.allocationRules);
  };
  const refreshProducts = async () => {
    const result = await readGraphql<{ productCatalog: ProductConcept[] }>(productCatalogQuery, {
      householdId: household.id,
    });
    setCatalog(result.productCatalog);
  };
  const refreshInvitations = async () => {
    if (!isOwner) return;
    const result = await readGraphql<{ householdInvitations: HouseholdInvitation[] }>(
      householdInvitationsQuery,
      { householdId: household.id },
    );
    setInvitations(result.householdInvitations);
  };
  useEffect(() => {
    setFundingDrafts(
      Object.fromEntries(
        household.pools.map((pool) => [
          pool.id,
          {
            ownerMemberId: pool.ownerMemberId ?? "",
            policy: pool.settlementPolicy ?? "BALANCE_TO_STANDARD",
            weights: Object.fromEntries(
              household.members.map((member) => [
                member.id,
                String(
                  pool.fundingShares.find((share) => share.memberId === member.id)?.weight ?? "",
                ),
              ]),
            ),
          },
        ]),
      ),
    );
  }, [household.id, household.pools, household.members]);
  useEffect(() => {
    setInvitationLink("");
    setIssuedInvitationId("");
    void Promise.all([refreshRules(), refreshProducts(), refreshInvitations()]).catch((reason) =>
      onNotice((reason as Error).message),
    );
  }, [household.id, isOwner]);
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
  const saveFundingRule = async (event: FormEvent, poolId: string) => {
    event.preventDefault();
    const pool = household.pools.find((item) => item.id === poolId);
    const draft = fundingDrafts[poolId];
    if (!pool || !draft) return;
    const fundingShares = household.members
      .map((member) => ({
        memberId: member.id,
        weight: Math.max(0, Number(draft.weights[member.id]) || 0),
      }))
      .filter((share) => share.weight > 0);
    if (pool.kind === "PERSONAL" && !draft.ownerMemberId) {
      onNotice("所有者を選択してください");
      return;
    }
    if (pool.kind === "SHARED" && fundingShares.length === 0) {
      onNotice("拠出比率を入力してください");
      return;
    }
    setPending(true);
    try {
      await readGraphql(setPoolFundingRuleMutation, {
        input: {
          householdId: household.id,
          poolId,
          ownerMemberId: pool.kind === "PERSONAL" ? draft.ownerMemberId : null,
          fundingShares: pool.kind === "SHARED" ? fundingShares : [],
          settlementPolicy: pool.kind === "SHARED" ? draft.policy : null,
          expectedVersion: pool.fundingRuleVersion,
        },
      });
      await onRefresh();
      onNotice("精算設定を保存しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
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
      (ruleScope === "MERCHANT" && !ruleMerchant.trim()) ||
      (ruleScope === "PRODUCT" && !ruleProductId) ||
      (ruleScope === "PRODUCT_VARIANT" && !ruleVariantId)
    )
      return;
    setPending(true);
    try {
      await readGraphql(setAllocationRuleMutation, {
        input: {
          householdId: household.id,
          ...(ruleScope === "CATEGORY"
            ? { categoryId: ruleCategoryId }
            : ruleScope === "MERCHANT"
              ? { merchant: ruleMerchant.trim() }
              : ruleScope === "PRODUCT"
                ? { productConceptId: ruleProductId }
                : { productVariantId: ruleVariantId }),
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
  const issueInvitation = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    try {
      const expiresAt = new Date(Date.now() + Number(expiresInDays) * 86_400_000).toISOString();
      const result = await readGraphql<{
        createHouseholdInvitation: { token: string; invitation: HouseholdInvitation };
      }>(createHouseholdInvitationMutation, { input: { householdId: household.id, expiresAt } });
      setInvitationLink(
        `${window.location.origin}/#invite=${encodeURIComponent(result.createHouseholdInvitation.token)}`,
      );
      setIssuedInvitationId(result.createHouseholdInvitation.invitation.id);
      await refreshInvitations();
      onNotice("招待を発行しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const revokeInvitation = async (invitationId: string) => {
    setPending(true);
    try {
      await readGraphql(revokeHouseholdInvitationMutation, {
        householdId: household.id,
        invitationId,
      });
      if (invitationId === issuedInvitationId) {
        setInvitationLink("");
        setIssuedInvitationId("");
      }
      await refreshInvitations();
      onNotice("招待を失効しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const createConcept = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    try {
      await readGraphql(createProductConceptMutation, {
        input: {
          householdId: household.id,
          name: conceptName.trim(),
          categoryId: conceptCategoryId || null,
        },
      });
      setConceptName("");
      await refreshProducts();
      onNotice("商品を追加しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const createVariant = async (event: FormEvent) => {
    event.preventDefault();
    if (!variantConceptId) return;
    setPending(true);
    try {
      await readGraphql(createProductVariantMutation, {
        input: {
          householdId: household.id,
          conceptId: variantConceptId,
          name: variantName.trim(),
          brand: variantBrand.trim() || null,
          packageSize: variantPackageSize.trim() || null,
        },
      });
      setVariantName("");
      setVariantBrand("");
      setVariantPackageSize("");
      await refreshProducts();
      onNotice("バリエーションを追加しました");
    } catch (reason) {
      onNotice((reason as Error).message);
    } finally {
      setPending(false);
    }
  };
  const createAlias = async (event: FormEvent) => {
    event.preventDefault();
    if (!aliasVariantId) return;
    setPending(true);
    try {
      await readGraphql(setProductAliasMutation, {
        input: {
          householdId: household.id,
          variantId: aliasVariantId,
          merchant: aliasMerchant.trim(),
          alias: aliasName.trim(),
        },
      });
      setAliasName("");
      await refreshProducts();
      onNotice("店名の別表記を登録しました");
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
              <div className="pool-funding-card" key={pool.id}>
                <div className="setting-row">
                  <div className="pool-avatar">{pool.name.slice(0, 1)}</div>
                  <div>
                    <b>{pool.name}</b>
                    <small>
                      {pool.kind === "SHARED" ? "共用" : "個人"} · 品目負担の標準比{" "}
                      {pool.defaultShareWeight}
                    </small>
                  </div>
                  {(pool.kind === "PERSONAL" && !pool.ownerMemberId) ||
                  (pool.kind === "SHARED" &&
                    (!pool.fundingShares.length || !pool.settlementPolicy)) ? (
                    <span className="period-chip">設定が必要</span>
                  ) : null}
                  <ChevronRight size={16} />
                </div>
                <form
                  className="pool-funding-form"
                  onSubmit={(event) => void saveFundingRule(event, pool.id)}
                >
                  {pool.kind === "PERSONAL" ? (
                    <label>
                      所有者
                      <select
                        required
                        value={fundingDrafts[pool.id]?.ownerMemberId ?? ""}
                        onChange={(event) =>
                          setFundingDrafts((current) => ({
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
                          value={fundingDrafts[pool.id]?.policy ?? "BALANCE_TO_STANDARD"}
                          onChange={(event) =>
                            setFundingDrafts((current) => ({
                              ...current,
                              [pool.id]: {
                                ...current[pool.id],
                                policy: event.target.value as SharedPoolSettlementPolicy,
                              },
                            }))
                          }
                        >
                          <option value="BALANCE_TO_STANDARD">負担割合の平準化</option>
                          <option value="REIMBURSE_PERSONAL">予算外支出の補填</option>
                        </select>
                      </label>
                      <div className="pool-funding-shares">
                        {household.members.map((member) => (
                          <label key={member.id}>
                            <span>{member.displayName}</span>
                            <div className="number-field">
                              <input
                                inputMode="numeric"
                                value={fundingDrafts[pool.id]?.weights[member.id] ?? ""}
                                onChange={(event) =>
                                  setFundingDrafts((current) => ({
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
                  <button className="button button-soft" disabled={pending}>
                    <Check size={15} />
                    精算設定を保存
                  </button>
                </form>
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
                <div className="pool-avatar">
                  {rule.scope === "CATEGORY"
                    ? "分"
                    : rule.scope === "MERCHANT"
                      ? "店"
                      : rule.scope === "PRODUCT"
                        ? "品"
                        : "規"}
                </div>
                <div>
                  <b>
                    {rule.scope === "CATEGORY"
                      ? (household.categories.find((category) => category.id === rule.categoryId)
                          ?.name ?? "カテゴリ")
                      : rule.scope === "MERCHANT"
                        ? rule.merchant
                        : rule.scope === "PRODUCT"
                          ? (catalog.find((product) => product.id === rule.productConceptId)
                              ?.name ?? "商品")
                          : (catalog
                              .flatMap((product) => product.variants)
                              .find((variant) => variant.id === rule.productVariantId)?.name ??
                            "バリエーション")}
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
              <option value="PRODUCT">商品</option>
              <option value="PRODUCT_VARIANT">バリエーション</option>
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
            ) : ruleScope === "MERCHANT" ? (
              <input
                value={ruleMerchant}
                onChange={(event) => setRuleMerchant(event.target.value)}
                placeholder="店名"
                required
              />
            ) : ruleScope === "PRODUCT" ? (
              <select
                value={ruleProductId}
                onChange={(event) => setRuleProductId(event.target.value)}
                required
              >
                <option value="">商品を選択</option>
                {catalog
                  .filter((product) => !product.archived)
                  .map((product) => (
                    <option key={product.id} value={product.id}>
                      {product.name}
                    </option>
                  ))}
              </select>
            ) : (
              <select
                value={ruleVariantId}
                onChange={(event) => setRuleVariantId(event.target.value)}
                required
              >
                <option value="">バリエーションを選択</option>
                {catalog.flatMap((product) =>
                  product.variants
                    .filter((variant) => !variant.archived)
                    .map((variant) => (
                      <option key={variant.id} value={variant.id}>
                        {product.name} · {variant.name}
                      </option>
                    )),
                )}
              </select>
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
        <section className="panel settings-section">
          <div className="panel-heading">
            <div>
              <span className="eyebrow">商品カタログ</span>
              <h2>商品とバリエーション</h2>
            </div>
            <span className="count-badge">{catalog.length}</span>
          </div>
          <div className="settings-rows">
            {catalog.map((product) => (
              <div className="setting-row" key={product.id}>
                <div className="pool-avatar">品</div>
                <div>
                  <b>{product.name}</b>
                  <small>
                    {product.variants
                      .map((variant) =>
                        [variant.name, variant.brand, variant.packageSize]
                          .filter(Boolean)
                          .join(" · "),
                      )
                      .join(" / ") || "バリエーションなし"}
                  </small>
                  {product.variants.some((variant) => variant.aliases.length > 0) && (
                    <small>
                      {product.variants
                        .flatMap((variant) =>
                          variant.aliases.map((alias) => `${alias.merchantKey}: ${alias.alias}`),
                        )
                        .join(" / ")}
                    </small>
                  )}
                </div>
              </div>
            ))}
          </div>
          <form className="inline-create" onSubmit={createConcept}>
            <input
              value={conceptName}
              onChange={(event) => setConceptName(event.target.value)}
              placeholder="商品名"
              required
            />
            <select
              value={conceptCategoryId}
              onChange={(event) => setConceptCategoryId(event.target.value)}
            >
              <option value="">カテゴリなし</option>
              {household.categories
                .filter((category) => !category.archived)
                .map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
            </select>
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
          <form className="allocation-rule-form" onSubmit={createVariant}>
            <select
              value={variantConceptId}
              onChange={(event) => setVariantConceptId(event.target.value)}
              required
            >
              <option value="">商品を選択</option>
              {catalog
                .filter((product) => !product.archived)
                .map((product) => (
                  <option key={product.id} value={product.id}>
                    {product.name}
                  </option>
                ))}
            </select>
            <input
              value={variantName}
              onChange={(event) => setVariantName(event.target.value)}
              placeholder="バリエーション名"
              required
            />
            <input
              value={variantBrand}
              onChange={(event) => setVariantBrand(event.target.value)}
              placeholder="ブランド"
            />
            <input
              value={variantPackageSize}
              onChange={(event) => setVariantPackageSize(event.target.value)}
              placeholder="内容量・規格"
            />
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              バリエーションを追加
            </button>
          </form>
          <form className="allocation-rule-form" onSubmit={createAlias}>
            <select
              value={aliasVariantId}
              onChange={(event) => setAliasVariantId(event.target.value)}
              required
            >
              <option value="">バリエーションを選択</option>
              {catalog.flatMap((product) =>
                product.variants
                  .filter((variant) => !variant.archived)
                  .map((variant) => (
                    <option key={variant.id} value={variant.id}>
                      {product.name} · {variant.name}
                    </option>
                  )),
              )}
            </select>
            <input
              value={aliasMerchant}
              onChange={(event) => setAliasMerchant(event.target.value)}
              placeholder="店名"
              required
            />
            <input
              value={aliasName}
              onChange={(event) => setAliasName(event.target.value)}
              placeholder="店頭の表記"
              required
            />
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              別表記を追加
            </button>
          </form>
        </section>
        {isOwner && (
          <section className="panel settings-section">
            <div className="panel-heading">
              <div>
                <span className="eyebrow">共同編集</span>
                <h2>招待</h2>
              </div>
              <span className="count-badge">
                {
                  invitations.filter(
                    (invitation) => !invitation.revokedAt && !invitation.acceptedAt,
                  ).length
                }
              </span>
            </div>
            <form className="inline-create" onSubmit={issueInvitation}>
              <label>
                有効期間
                <select
                  value={expiresInDays}
                  onChange={(event) => setExpiresInDays(event.target.value)}
                >
                  <option value="1">1日</option>
                  <option value="7">7日</option>
                  <option value="30">30日</option>
                </select>
              </label>
              <button className="button button-primary" disabled={pending}>
                <Plus size={15} />
                招待リンクを発行
              </button>
            </form>
            {invitationLink && (
              <div className="invitation-issued">
                <input aria-label="招待リンク" readOnly value={invitationLink} />
                <button
                  type="button"
                  className="button button-soft"
                  onClick={() =>
                    void navigator.clipboard
                      .writeText(invitationLink)
                      .then(() => onNotice("招待リンクをコピーしました"))
                      .catch(() => onNotice("招待リンクをコピーできませんでした"))
                  }
                >
                  リンクをコピー
                </button>
              </div>
            )}
            <div className="settings-rows">
              {invitations.map((invitation) => (
                <div className="setting-row" key={invitation.id}>
                  <div className="pool-avatar">招</div>
                  <div>
                    <b>{new Date(invitation.expiresAt).toLocaleDateString("ja-JP")}まで</b>
                    <small>
                      {invitation.acceptedAt
                        ? "参加済み"
                        : invitation.revokedAt
                          ? "失効"
                          : "未使用"}
                    </small>
                  </div>
                  {!invitation.acceptedAt && !invitation.revokedAt && (
                    <button
                      className="text-button"
                      disabled={pending}
                      onClick={() => void revokeInvitation(invitation.id)}
                    >
                      失効
                    </button>
                  )}
                </div>
              ))}
            </div>
          </section>
        )}
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

import { useEffect, useState } from "react";
import { BookOpen, ChevronDown, ChevronRight, Plus, Search } from "lucide-react";
import {
  ApiError,
  purchasesQuery,
  productCatalogQuery,
  readGraphql,
  updatePurchaseItemMutation,
  updatePurchaseMutation,
  type Connection,
  type Household,
  type Purchase,
  type PurchaseItem,
  type ProductConcept,
} from "../../../shared/api";
import { EmptyState, InlineError } from "../../../shared/ui";
import { useHouseholdPurchases } from "../../../shared/api";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sameNullableString = (left?: string | null, right?: string | null) =>
  (left ?? "") === (right ?? "");

export function LedgerPage({ household, onAdd }: { household: Household; onAdd: () => void }) {
  const { purchases, loading, error, refresh } = useHouseholdPurchases(household.id, 100);
  const [search, setSearch] = useState("");
  const [month, setMonth] = useState("");
  const [expanded, setExpanded] = useState<string | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [purchaseDraft, setPurchaseDraft] = useState<{
    merchant: string;
    occurredAt: string;
    paymentAccountId: string;
  } | null>(null);
  const [itemDrafts, setItemDrafts] = useState<
    Record<
      string,
      {
        name: string;
        amountYen: string;
        categoryId: string;
        productVariantId: string;
        productLearning: "ITEM_ONLY" | "VARIANT_NAME" | "MERCHANT_ALIAS";
        variantName: string;
      }
    >
  >({});
  const [catalog, setCatalog] = useState<ProductConcept[]>([]);
  const [editError, setEditError] = useState("");
  const [conflict, setConflict] = useState<{
    label: string;
    localValue: string;
    serverValue: string;
    retry: () => Promise<void>;
    discard: () => Promise<void>;
  } | null>(null);
  const latestPurchase = async (purchaseId: string) => {
    const result = await readGraphql<{ purchases: Connection<Purchase> }>(purchasesQuery, {
      householdId: household.id,
      first: 100,
    });
    return result.purchases.edges
      .map((edge) => edge.node)
      .find((purchase) => purchase.id === purchaseId);
  };
  useEffect(() => {
    let active = true;
    void readGraphql<{ productCatalog: ProductConcept[] }>(productCatalogQuery, {
      householdId: household.id,
    })
      .then((result) => active && setCatalog(result.productCatalog))
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [household.id]);
  const startEditing = (purchase: Purchase) => {
    const occurredAt = new Date(purchase.occurredAt);
    occurredAt.setMinutes(occurredAt.getMinutes() - occurredAt.getTimezoneOffset());
    setEditing(purchase.id);
    setPurchaseDraft({
      merchant: purchase.merchant,
      occurredAt: occurredAt.toISOString().slice(0, 16),
      paymentAccountId: purchase.paymentAccountId ?? "",
    });
    setItemDrafts(
      Object.fromEntries(
        purchase.items.map((item) => [
          item.id,
          {
            name: item.name,
            amountYen: String(item.amountYen),
            categoryId: item.categoryId ?? "",
            productVariantId: item.productVariantId ?? "",
            productLearning: "ITEM_ONLY",
            variantName: "",
          },
        ]),
      ),
    );
    setConflict(null);
    setEditError("");
  };
  const saveItem = async (purchase: Purchase, item: PurchaseItem) => {
    const draft = itemDrafts[item.id];
    if (!draft || !draft.name.trim() || Number(draft.amountYen) < 1) return;
    const patch = {
      name: draft.name.trim(),
      amountYen: Number(draft.amountYen),
      categoryId: draft.categoryId || null,
      productVariantId: draft.productVariantId || null,
    };
    const changedFields = [
      ...(patch.name !== item.name ? ["name"] : []),
      ...(patch.amountYen !== item.amountYen ? ["amountYen"] : []),
      ...(!sameNullableString(patch.categoryId, item.categoryId) ? ["categoryId"] : []),
      ...(!sameNullableString(patch.productVariantId, item.productVariantId)
        ? ["productVariantId"]
        : []),
    ] as Array<keyof typeof patch>;
    if (!changedFields.length) return;
    const changedPatch = Object.fromEntries(changedFields.map((field) => [field, patch[field]]));
    try {
      await readGraphql(updatePurchaseItemMutation, {
        input: {
          itemId: item.id,
          baseRevision: item.revision,
          ...changedPatch,
          ...(changedFields.includes("productVariantId") && patch.productVariantId
            ? {
                productLearning: draft.productLearning,
                ...(draft.productLearning === "VARIANT_NAME"
                  ? { variantName: draft.variantName.trim() }
                  : {}),
              }
            : {}),
          changedFields,
        },
      });
      await refresh();
      setEditError("");
    } catch (reason) {
      if (
        (reason instanceof ApiError && reason.code === "CONFLICT") ||
        (reason as Error).message.toLowerCase().includes("conflict")
      ) {
        const latest = await latestPurchase(purchase.id);
        const current = latest?.items.find((row) => row.id === item.id);
        await refresh();
        if (current)
          setConflict({
            label: `${item.name}の品目`,
            localValue: changedFields
              .map((field) => `${field}: ${String(patch[field as keyof typeof patch] ?? "未分類")}`)
              .join("、"),
            serverValue: `${current.name} · ${yen(current.amountYen)}`,
            retry: async () => {
              await readGraphql(updatePurchaseItemMutation, {
                input: {
                  itemId: current.id,
                  baseRevision: current.revision,
                  ...changedPatch,
                  ...(changedFields.includes("productVariantId") && patch.productVariantId
                    ? {
                        productLearning: draft.productLearning,
                        ...(draft.productLearning === "VARIANT_NAME"
                          ? { variantName: draft.variantName.trim() }
                          : {}),
                      }
                    : {}),
                  changedFields,
                },
              });
              await refresh();
              setConflict(null);
            },
            discard: async () => {
              await refresh();
              setConflict(null);
            },
          });
      } else setEditError((reason as Error).message);
    }
  };
  const savePurchase = async (purchase: Purchase) => {
    if (!purchaseDraft) return;
    const patch = {
      merchant: purchaseDraft.merchant.trim(),
      occurredAt: new Date(purchaseDraft.occurredAt).toISOString(),
      paymentAccountId: purchaseDraft.paymentAccountId || null,
    };
    const changedFields = [
      ...(patch.merchant !== purchase.merchant ? ["merchant"] : []),
      ...(Date.parse(patch.occurredAt) !== Date.parse(purchase.occurredAt) ? ["occurredAt"] : []),
      ...(!sameNullableString(patch.paymentAccountId, purchase.paymentAccountId)
        ? ["paymentAccountId"]
        : []),
    ] as Array<keyof typeof patch>;
    if (!changedFields.length) return;
    const changedPatch = Object.fromEntries(changedFields.map((field) => [field, patch[field]]));
    try {
      await readGraphql(updatePurchaseMutation, {
        input: {
          purchaseId: purchase.id,
          baseRevision: purchase.revision,
          patch: changedPatch,
          changedFields,
        },
      });
      await refresh();
      setEditing(null);
      setConflict(null);
      setEditError("");
    } catch (reason) {
      if (
        (reason instanceof ApiError && reason.code === "CONFLICT") ||
        (reason as Error).message.toLowerCase().includes("conflict")
      ) {
        const latest = await latestPurchase(purchase.id);
        await refresh();
        if (latest)
          setConflict({
            label: "買い物の情報",
            localValue: `${patch.merchant} · ${new Date(patch.occurredAt).toLocaleString("ja-JP")}`,
            serverValue: `${latest.merchant} · ${new Date(latest.occurredAt).toLocaleString("ja-JP")}`,
            retry: async () => {
              await readGraphql(updatePurchaseMutation, {
                input: {
                  purchaseId: latest.id,
                  baseRevision: latest.revision,
                  patch: changedPatch,
                  changedFields,
                },
              });
              await refresh();
              setConflict(null);
            },
            discard: async () => {
              await refresh();
              setConflict(null);
              setEditing(null);
            },
          });
      } else setEditError((reason as Error).message);
    }
  };
  const filtered = purchases.filter(
    (purchase) =>
      (!month || purchase.occurredAt.slice(0, 7) === month) &&
      (!search ||
        `${purchase.merchant} ${purchase.items.map((item) => item.name).join(" ")}`
          .toLocaleLowerCase()
          .includes(search.toLocaleLowerCase())),
  );
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">購入と品目</span>
          <h1>帳簿</h1>
          <p>買い物の記録をまとめて確認</p>
        </div>
        <button className="button button-primary" onClick={onAdd}>
          <Plus size={17} />
          買い物を追加
        </button>
      </div>
      <div className="ledger-controls">
        <label className="input-with-icon">
          <Search size={16} />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="店名、品名で検索"
          />
        </label>
        <label className="select-filter">
          <span>期間</span>
          <input type="month" value={month} onChange={(e) => setMonth(e.target.value)} />
        </label>
        <div className="record-count">{filtered.length}件</div>
      </div>
      {editError && <InlineError message={editError} />}
      {conflict && (
        <section className="panel conflict-panel" aria-live="polite">
          <div>
            <b>{conflict.label}が別の更新を受けています</b>
            <span>あなたの変更: {conflict.localValue}</span>
            <span>最新の記録: {conflict.serverValue}</span>
          </div>
          <div className="conflict-actions">
            <button className="button button-soft" onClick={() => void conflict.discard()}>
              最新の記録を使う
            </button>
            <button
              className="button button-primary"
              onClick={() =>
                void conflict.retry().catch((reason) => setEditError((reason as Error).message))
              }
            >
              自分の変更を再適用
            </button>
          </div>
        </section>
      )}
      <section className="panel ledger-list">
        {loading ? (
          <div className="skeleton-list">
            <i />
            <i />
            <i />
          </div>
        ) : error ? (
          <InlineError message={error} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={<BookOpen />}
            title="買い物が見つかりません"
            action="買い物を追加"
            onClick={onAdd}
          />
        ) : (
          filtered.map((purchase) => (
            <div
              key={purchase.id}
              className={`ledger-entry ${expanded === purchase.id ? "ledger-expanded" : ""}`}
            >
              <button
                className="ledger-purchase"
                onClick={() => setExpanded(expanded === purchase.id ? null : purchase.id)}
              >
                <div className="merchant-icon">{purchase.merchant?.slice(0, 1) || "買"}</div>
                <div className="purchase-description">
                  <b>{purchase.merchant || "店舗未入力"}</b>
                  <span>
                    {new Date(purchase.occurredAt).toLocaleString("ja-JP", {
                      month: "numeric",
                      day: "numeric",
                      hour: "2-digit",
                      minute: "2-digit",
                    })}{" "}
                    · {purchase.items.length}品目 ·{" "}
                    {purchase.paymentAccount?.name ?? "決済元未設定"}
                  </span>
                </div>
                <div className="purchase-meta">
                  <b>{yen(purchase.totalYen)}</b>
                  <span>{purchase.status === "CLOSED" ? "締め済み" : "記録済み"}</span>
                </div>
                {expanded === purchase.id ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
              </button>
              {expanded === purchase.id && (
                <div className="expanded-items">
                  {editing === purchase.id && purchaseDraft ? (
                    <form
                      className="purchase-edit-form"
                      onSubmit={(event) => {
                        event.preventDefault();
                        void savePurchase(purchase);
                      }}
                    >
                      <input
                        aria-label="店名"
                        value={purchaseDraft.merchant}
                        onChange={(event) =>
                          setPurchaseDraft({ ...purchaseDraft, merchant: event.target.value })
                        }
                      />
                      <input
                        aria-label="購入日時"
                        type="datetime-local"
                        value={purchaseDraft.occurredAt}
                        onChange={(event) =>
                          setPurchaseDraft({ ...purchaseDraft, occurredAt: event.target.value })
                        }
                      />
                      <select
                        aria-label="決済元"
                        value={purchaseDraft.paymentAccountId}
                        onChange={(event) =>
                          setPurchaseDraft({
                            ...purchaseDraft,
                            paymentAccountId: event.target.value,
                          })
                        }
                      >
                        <option value="">決済元未設定</option>
                        {household.accounts.map((account) => (
                          <option key={account.id} value={account.id}>
                            {account.name}
                          </option>
                        ))}
                      </select>
                      <div className="edit-actions">
                        <button
                          type="button"
                          className="button button-soft"
                          onClick={() => setEditing(null)}
                        >
                          キャンセル
                        </button>
                        <button className="button button-primary">買い物を保存</button>
                      </div>
                    </form>
                  ) : (
                    <button className="text-button" onClick={() => startEditing(purchase)}>
                      買い物を編集
                    </button>
                  )}
                  {purchase.items.map((item) => (
                    <div className="expanded-item" key={item.id}>
                      {editing === purchase.id ? (
                        <>
                          <input
                            aria-label="品名"
                            value={itemDrafts[item.id]?.name ?? item.name}
                            onChange={(event) =>
                              setItemDrafts((current) => ({
                                ...current,
                                [item.id]: { ...current[item.id], name: event.target.value },
                              }))
                            }
                          />
                          <select
                            aria-label="カテゴリ"
                            value={itemDrafts[item.id]?.categoryId ?? item.categoryId ?? ""}
                            onChange={(event) =>
                              setItemDrafts((current) => ({
                                ...current,
                                [item.id]: { ...current[item.id], categoryId: event.target.value },
                              }))
                            }
                          >
                            <option value="">未分類</option>
                            {household.categories
                              .filter((category) => !category.archived)
                              .map((category) => (
                                <option key={category.id} value={category.id}>
                                  {category.name}
                                </option>
                              ))}
                          </select>
                          <select
                            aria-label="正規商品"
                            value={
                              itemDrafts[item.id]?.productVariantId ?? item.productVariantId ?? ""
                            }
                            onChange={(event) =>
                              setItemDrafts((current) => ({
                                ...current,
                                [item.id]: {
                                  ...current[item.id],
                                  productVariantId: event.target.value,
                                  variantName:
                                    catalog
                                      .flatMap((concept) => concept.variants)
                                      .find((variant) => variant.id === event.target.value)?.name ??
                                    "",
                                },
                              }))
                            }
                          >
                            <option value="">商品未選択</option>
                            {catalog.flatMap((concept) =>
                              concept.variants
                                .filter((variant) => !variant.archived)
                                .map((variant) => (
                                  <option key={variant.id} value={variant.id}>
                                    {concept.name} · {variant.name}
                                  </option>
                                )),
                            )}
                          </select>
                          {itemDrafts[item.id]?.productVariantId !==
                            (item.productVariantId ?? "") &&
                            itemDrafts[item.id]?.productVariantId && (
                              <>
                                <select
                                  aria-label="商品名の学習範囲"
                                  value={itemDrafts[item.id]?.productLearning ?? "ITEM_ONLY"}
                                  onChange={(event) =>
                                    setItemDrafts((current) => ({
                                      ...current,
                                      [item.id]: {
                                        ...current[item.id],
                                        productLearning: event.target.value as
                                          | "ITEM_ONLY"
                                          | "VARIANT_NAME"
                                          | "MERCHANT_ALIAS",
                                      },
                                    }))
                                  }
                                >
                                  <option value="ITEM_ONLY">この品目だけ</option>
                                  <option value="VARIANT_NAME">バリエーション名として学習</option>
                                  <option value="MERCHANT_ALIAS">この店の別表記として学習</option>
                                </select>
                                {itemDrafts[item.id]?.productLearning === "VARIANT_NAME" && (
                                  <input
                                    aria-label="バリエーション名"
                                    value={itemDrafts[item.id]?.variantName ?? ""}
                                    onChange={(event) =>
                                      setItemDrafts((current) => ({
                                        ...current,
                                        [item.id]: {
                                          ...current[item.id],
                                          variantName: event.target.value,
                                        },
                                      }))
                                    }
                                    placeholder="バリエーション名"
                                  />
                                )}
                              </>
                            )}
                          <input
                            aria-label="金額"
                            inputMode="numeric"
                            value={itemDrafts[item.id]?.amountYen ?? String(item.amountYen)}
                            onChange={(event) =>
                              setItemDrafts((current) => ({
                                ...current,
                                [item.id]: {
                                  ...current[item.id],
                                  amountYen: event.target.value.replace(/\D/g, ""),
                                },
                              }))
                            }
                          />
                          <button
                            className="text-button"
                            type="button"
                            onClick={() => void saveItem(purchase, item)}
                          >
                            品目を保存
                          </button>
                        </>
                      ) : (
                        <>
                          <span>{item.name}</span>
                          <span>
                            {household.categories.find(
                              (category) => category.id === item.categoryId,
                            )?.name ?? "未分類"}
                          </span>
                          <b>{yen(item.amountYen)}</b>
                          <span>
                            {item.allocations
                              .map(
                                (allocation) =>
                                  `${allocation.poolName ?? household.pools.find((pool) => pool.id === allocation.poolId)?.name ?? ""} ${allocation.weight}`,
                              )
                              .join(" / ")}
                          </span>
                        </>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))
        )}
      </section>
    </>
  );
}

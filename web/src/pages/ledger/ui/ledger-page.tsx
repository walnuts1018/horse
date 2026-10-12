import { useEffect, useState } from "react";
import { BookOpen, ChevronDown, ChevronRight, Plus, Search, X } from "lucide-react";
import {
  ApiError,
  correctPurchaseMutation,
  makeIdempotencyKey,
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
  type PurchasePaymentInput,
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
    paymentAllocations: Array<{ paymentAccountId: string; amountYen: string }>;
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
  const [correctionTarget, setCorrectionTarget] = useState<{
    purchaseId: string;
    itemId: string;
  } | null>(null);
  const [refundAmount, setRefundAmount] = useState("");
  const [refundReason, setRefundReason] = useState("");
  const [refundPayments, setRefundPayments] = useState<
    Array<{ key: string; paymentAccountId: string; amountYen: string }>
  >([]);
  const [refundAutoTotal, setRefundAutoTotal] = useState(true);
  const [correctionPending, setCorrectionPending] = useState(false);
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
      paymentAllocations: purchase.paymentAllocations?.length
        ? purchase.paymentAllocations.map((payment) => ({
            paymentAccountId: payment.paymentAccountId,
            amountYen: String(payment.amountYen),
          }))
        : purchase.paymentAccountId
          ? [{ paymentAccountId: purchase.paymentAccountId, amountYen: String(purchase.totalYen) }]
          : [],
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
  const startCorrection = (purchase: Purchase, item: PurchaseItem) => {
    setCorrectionTarget({ purchaseId: purchase.id, itemId: item.id });
    setRefundAmount("");
    setRefundReason("");
    setRefundPayments([]);
    setRefundAutoTotal(true);
    setEditError("");
  };
  const addRefundPayment = () => {
    setRefundPayments((current) => [
      ...current.map((payment) => ({
        ...payment,
        amountYen: refundAutoTotal && current.length === 1 ? refundAmount : payment.amountYen,
      })),
      {
        key: crypto.randomUUID(),
        paymentAccountId: "",
        amountYen: current.length === 0 ? refundAmount : "",
      },
    ]);
    setRefundAutoTotal(false);
  };
  const submitCorrection = async (purchase: Purchase, item: PurchaseItem) => {
    const amountYen = Number(refundAmount);
    const previousCorrections = (purchase.corrections ?? [])
      .filter((correction) => correction.itemId === item.id)
      .reduce((sum, correction) => sum + correction.amountDeltaYen, 0);
    const remaining = item.amountYen + previousCorrections;
    const refundTotal = refundPayments.reduce(
      (sum, payment) =>
        sum +
        Number(refundAutoTotal && refundPayments.length === 1 ? refundAmount : payment.amountYen),
      0,
    );
    const paymentAllocations: PurchasePaymentInput[] = refundPayments.map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen: Number(
        refundAutoTotal && refundPayments.length === 1 ? refundAmount : payment.amountYen,
      ),
    }));
    if (!Number.isSafeInteger(amountYen) || amountYen < 1 || amountYen > remaining) {
      setEditError("返金額を確認してください");
      return;
    }
    if (
      refundPayments.some(
        (payment, index) =>
          !payment.paymentAccountId ||
          !Number.isSafeInteger(paymentAllocations[index].amountYen) ||
          paymentAllocations[index].amountYen < 1,
      ) ||
      (paymentAllocations.length > 0 && refundTotal !== amountYen)
    ) {
      setEditError("返金先と返金額を確認してください");
      return;
    }
    if (!refundReason.trim()) {
      setEditError("理由を入力してください");
      return;
    }
    const allocations = item.allocations
      .filter((allocation) => allocation.weight > 0)
      .map(({ poolId, weight }) => ({ poolId, weight }));
    if (!allocations.length) {
      setEditError("元品目の負担先がありません");
      return;
    }
    setCorrectionPending(true);
    setEditError("");
    try {
      await readGraphql(correctPurchaseMutation, {
        input: {
          householdId: household.id,
          purchaseId: purchase.id,
          itemId: item.id,
          amountDeltaYen: -amountYen,
          reason: refundReason.trim(),
          allocations,
          paymentAllocations,
          idempotencyKey: makeIdempotencyKey(),
        },
      });
      setCorrectionTarget(null);
      await refresh();
    } catch (reason) {
      setEditError((reason as Error).message);
    } finally {
      setCorrectionPending(false);
    }
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
    const paymentAllocations: PurchasePaymentInput[] = purchaseDraft.paymentAllocations.map(
      (payment) => ({
        paymentAccountId: payment.paymentAccountId,
        amountYen: Number(payment.amountYen),
      }),
    );
    const currentAllocations = (
      purchase.paymentAllocations?.length
        ? purchase.paymentAllocations
        : purchase.paymentAccountId
          ? [{ paymentAccountId: purchase.paymentAccountId, amountYen: purchase.totalYen }]
          : []
    ).map((payment) => ({
      paymentAccountId: payment.paymentAccountId,
      amountYen: payment.amountYen,
    }));
    const paymentAllocationsChanged =
      JSON.stringify(paymentAllocations) !== JSON.stringify(currentAllocations);
    if (
      paymentAllocationsChanged &&
      (paymentAllocations.some(
        (payment) =>
          !payment.paymentAccountId ||
          !Number.isSafeInteger(payment.amountYen) ||
          payment.amountYen < 1,
      ) ||
        paymentAllocations.reduce((total, payment) => total + payment.amountYen, 0) !==
          purchase.totalYen)
    ) {
      setEditError("決済元と金額を確認してください");
      return;
    }
    const patch = {
      merchant: purchaseDraft.merchant.trim(),
      occurredAt: new Date(purchaseDraft.occurredAt).toISOString(),
      paymentAllocations,
    };
    const changedFields = [
      ...(patch.merchant !== purchase.merchant ? ["merchant"] : []),
      ...(Date.parse(patch.occurredAt) !== Date.parse(purchase.occurredAt) ? ["occurredAt"] : []),
      ...(paymentAllocationsChanged ? ["paymentAllocations"] : []),
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
  const refundPaymentTotal = refundPayments.reduce(
    (sum, payment) =>
      sum +
      Number(refundAutoTotal && refundPayments.length === 1 ? refundAmount : payment.amountYen),
    0,
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
                    {purchase.paymentAllocations?.length
                      ? purchase.paymentAllocations
                          .map((payment) => {
                            const accountName =
                              payment.account?.name ??
                              household.accounts.find(
                                (account) => account.id === payment.paymentAccountId,
                              )?.name ?? "決済元";
                            return `${accountName} ${yen(payment.amountYen)}`;
                          })
                          .join(" / ")
                      : purchase.paymentAccount?.name ?? "決済元未設定"}
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
                      <div className="purchase-edit-payments">
                        <b>決済内訳</b>
                        {purchaseDraft.paymentAllocations.map((payment, index) => (
                          <div className="purchase-edit-payment-row" key={index}>
                            <select
                              aria-label={`決済元 ${index + 1}`}
                              value={payment.paymentAccountId}
                              onChange={(event) =>
                                setPurchaseDraft({
                                  ...purchaseDraft,
                                  paymentAllocations: purchaseDraft.paymentAllocations.map(
                                    (row, rowIndex) =>
                                      rowIndex === index
                                        ? { ...row, paymentAccountId: event.target.value }
                                        : row,
                                  ),
                                })
                              }
                            >
                              <option value="">決済元を選択</option>
                              {household.accounts.map((account) => (
                                <option key={account.id} value={account.id}>
                                  {account.name}
                                </option>
                              ))}
                            </select>
                            <input
                              aria-label={`決済額 ${index + 1}`}
                              inputMode="numeric"
                              value={payment.amountYen}
                              onChange={(event) =>
                                setPurchaseDraft({
                                  ...purchaseDraft,
                                  paymentAllocations: purchaseDraft.paymentAllocations.map(
                                    (row, rowIndex) =>
                                      rowIndex === index
                                        ? { ...row, amountYen: event.target.value.replace(/\D/g, "") }
                                        : row,
                                  ),
                                })
                              }
                            />
                            <button
                              className="row-remove"
                              type="button"
                              aria-label={`決済元 ${index + 1} を削除`}
                              onClick={() =>
                                setPurchaseDraft({
                                  ...purchaseDraft,
                                  paymentAllocations: purchaseDraft.paymentAllocations.filter(
                                    (_, rowIndex) => rowIndex !== index,
                                  ),
                                })
                              }
                            >
                              <X size={14} />
                            </button>
                          </div>
                        ))}
                        <button
                          className="text-button"
                          type="button"
                          onClick={() =>
                            setPurchaseDraft({
                              ...purchaseDraft,
                              paymentAllocations: [
                                ...purchaseDraft.paymentAllocations,
                                { paymentAccountId: "", amountYen: "" },
                              ],
                            })
                          }
                        >
                          <Plus size={14} />
                          決済元を追加
                        </button>
                        <small>
                          差額 {yen(
                            purchase.totalYen -
                              purchaseDraft.paymentAllocations.reduce(
                                (total, payment) => total + Number(payment.amountYen || 0),
                                0,
                              ),
                          )}
                        </small>
                      </div>
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
                  {(purchase.corrections ?? []).map((correction) => (
                    <div className="purchase-correction-entry" key={correction.id}>
                      <span>{correction.itemId ? "品目返品" : "購入訂正"}</span>
                      <b
                        className={correction.amountDeltaYen < 0 ? "settlement-credit" : undefined}
                      >
                        {yen(correction.amountDeltaYen)}
                      </b>
                      <small>{correction.reason}</small>
                    </div>
                  ))}
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
                      {editing !== purchase.id &&
                        (() => {
                          const priorRefunds = (purchase.corrections ?? [])
                            .filter((correction) => correction.itemId === item.id)
                            .reduce((total, correction) => total + correction.amountDeltaYen, 0);
                          const remaining = item.amountYen + priorRefunds;
                          const isTarget =
                            correctionTarget?.purchaseId === purchase.id &&
                            correctionTarget.itemId === item.id;
                          return isTarget ? (
                            <form
                              className="purchase-correction-form"
                              onSubmit={(event) => {
                                event.preventDefault();
                                void submitCorrection(purchase, item);
                              }}
                            >
                              <b>{item.name}の返品</b>
                              <span>残額 {yen(Math.max(0, remaining))}</span>
                              <label>
                                返金額
                                <input
                                  aria-label="返金額"
                                  inputMode="numeric"
                                  value={refundAmount}
                                  max={remaining}
                                  onChange={(event) =>
                                    setRefundAmount(event.target.value.replace(/\D/g, ""))
                                  }
                                  required
                                />
                              </label>
                              <label>
                                理由
                                <input
                                  aria-label="返品理由"
                                  value={refundReason}
                                  onChange={(event) => setRefundReason(event.target.value)}
                                  required
                                />
                              </label>
                              {refundPayments.length === 0 ? (
                                <span>返金先なし</span>
                              ) : (
                                <div className="refund-payment-list">
                                  {refundPayments.map((payment, index) => (
                                    <div className="refund-payment-row" key={payment.key}>
                                      <select
                                        aria-label={`返金先 ${index + 1}`}
                                        value={payment.paymentAccountId}
                                        onChange={(event) =>
                                          setRefundPayments((current) =>
                                            current.map((row) =>
                                              row.key === payment.key
                                                ? { ...row, paymentAccountId: event.target.value }
                                                : row,
                                            ),
                                          )
                                        }
                                      >
                                        <option value="">口座を選択</option>
                                        {household.accounts.map((account) => (
                                          <option key={account.id} value={account.id}>
                                            {account.name}
                                          </option>
                                        ))}
                                      </select>
                                      <input
                                        aria-label={`返金額 ${index + 1}`}
                                        inputMode="numeric"
                                        value={
                                          refundAutoTotal && refundPayments.length === 1
                                            ? refundAmount
                                            : payment.amountYen
                                        }
                                        onChange={(event) => {
                                          setRefundAutoTotal(false);
                                          setRefundPayments((current) =>
                                            current.map((row) =>
                                              row.key === payment.key
                                                ? {
                                                    ...row,
                                                    amountYen: event.target.value.replace(
                                                      /\D/g,
                                                      "",
                                                    ),
                                                  }
                                                : row,
                                            ),
                                          );
                                        }}
                                      />
                                      <button
                                        className="row-remove"
                                        type="button"
                                        aria-label={`返金先 ${index + 1} を削除`}
                                        onClick={() =>
                                          setRefundPayments((current) =>
                                            current.filter((row) => row.key !== payment.key),
                                          )
                                        }
                                      >
                                        <X size={14} />
                                      </button>
                                    </div>
                                  ))}
                                </div>
                              )}
                              <button
                                className="text-button"
                                type="button"
                                onClick={addRefundPayment}
                              >
                                <Plus size={14} />
                                返金先を追加
                              </button>
                              {refundPayments.length > 0 && (
                                <span className="payment-difference">
                                  返金差額 {yen(Number(refundAmount) - refundPaymentTotal)}
                                </span>
                              )}
                              <div className="edit-actions">
                                <button
                                  className="button button-soft"
                                  type="button"
                                  onClick={() => setCorrectionTarget(null)}
                                >
                                  キャンセル
                                </button>
                                <button
                                  className="button button-primary"
                                  disabled={correctionPending || remaining <= 0}
                                >
                                  返品を記録
                                </button>
                              </div>
                            </form>
                          ) : (
                            <button
                              className="text-button correction-trigger"
                              type="button"
                              disabled={remaining <= 0}
                              onClick={() => startCorrection(purchase, item)}
                            >
                              返品を記録
                            </button>
                          );
                        })()}
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

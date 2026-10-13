import { useCallback, useEffect, useRef, useState } from "react";
import { BookOpen, ChevronDown, ChevronRight, Plus, Search } from "lucide-react";
import {
  ApiError,
  makeIdempotencyKey,
  readGraphql,
  useGraphqlMutation,
  type Connection,
  type Household,
  type Purchase,
  type PurchaseItem,
  type ProductConcept,
  type PurchasePaymentInput,
  type Tag,
  type SourceKind,
  type UnmatchedObservationState,
} from "../../../shared/api";
import {
  bulkUpdatePurchaseItemsMutation,
  postPurchaseMutation,
  purchasesQuery,
  mapUpdatePurchaseItemInput,
  updatePurchaseMutation,
  useUpdatePurchaseItem,
  useHouseholdPurchases,
} from "../../../entities/purchase";
import { readProductCatalog } from "../../../entities/product";
import { readTags } from "../../../entities/tag";
import { EmptyState, InlineError } from "../../../shared/ui";
import { formatQuantity, parseQuantity } from "../../../shared/lib";
import { createPurchaseDraft, type PurchaseDraft } from "../model/purchase-draft";
import { getRemainingRefundAmount } from "../model/purchase-correction";
import { usePurchaseBulkEdit } from "../model/purchase-bulk-edit";
import { usePurchaseEvidence } from "../model/use-purchase-evidence";
import { ObservationLedger } from "./observation-ledger";
import { PurchaseBulkActions } from "./purchase-bulk-actions";
import { PurchaseCorrection } from "./purchase-correction";
import { PurchaseEvidence } from "./purchase-evidence";
import { PurchaseEditForm } from "./purchase-edit-form";
import { toPurchaseAmountVariables, type LedgerSearch } from "../model/ledger-search";
import styles from "./ledger-search.module.css";

const yen = (amount = 0) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);
const sameNullableString = (left?: string | null, right?: string | null) =>
  (left ?? "") === (right ?? "");
const purchaseMonth = (occurredAt: string) =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
  }).format(new Date(occurredAt));
const nextMonth = (month: string) => {
  const [year, monthNumber] = month.split("-").map(Number);
  const serial = year * 12 + monthNumber;
  return `${Math.floor(serial / 12)}-${String((serial % 12) + 1).padStart(2, "0")}`;
};

export function LedgerPage({
  household,
  filters,
  onFiltersChange,
  onAdd,
  onUnsavedChanges,
}: {
  household: Household;
  filters: LedgerSearch;
  onFiltersChange: (updates: Partial<LedgerSearch>) => void;
  onAdd: () => void;
  onUnsavedChanges: (unsaved: boolean) => void;
}) {
  useEffect(() => {
    onUnsavedChanges(false);
    return () => onUnsavedChanges(false);
  }, [onUnsavedChanges]);
  const { mutate: mutatePostPurchase } = useGraphqlMutation(postPurchaseMutation);
  const mutateUpdatePurchaseItem = useUpdatePurchaseItem();
  const { mutate: mutateUpdatePurchase } = useGraphqlMutation(updatePurchaseMutation);
  const { mutate: mutateBulkUpdatePurchaseItems } = useGraphqlMutation<{
    bulkUpdatePurchaseItems: Array<{ id: string; revision: number }>;
  }>(bulkUpdatePurchaseItemsMutation);
  const {
    recordMode,
    viewMode,
    search,
    fromMonth,
    toMonth,
    categoryFilter,
    poolFilter,
    accountFilter,
    tagFilter,
    originFilter,
    evidenceFilter,
    observationSourceFilter,
    observationStateFilter,
    minimumAmount,
    maximumAmount,
  } = filters;
  const updateFilters = (updates: Partial<LedgerSearch>) => onFiltersChange(updates);
  const filtersActive = Boolean(
    search ||
    fromMonth ||
    toMonth ||
    categoryFilter ||
    poolFilter ||
    accountFilter ||
    tagFilter ||
    originFilter ||
    evidenceFilter ||
    minimumAmount ||
    maximumAmount,
  );
  const { purchases, loadingMore, moreError, hasNextPage, totalCount, loadMore, refresh } =
    useHouseholdPurchases(household.id, 100, {
      from: fromMonth ? `${fromMonth}-01T00:00:00+09:00` : undefined,
      to: toMonth ? `${nextMonth(toMonth)}-01T00:00:00+09:00` : undefined,
      origin: recordMode === "purchases" ? originFilter || undefined : undefined,
      hasEvidence:
        recordMode === "purchases" && evidenceFilter !== "" ? evidenceFilter === "true" : undefined,
      search: recordMode === "purchases" ? search || undefined : undefined,
      categoryId:
        recordMode === "purchases" && categoryFilter && categoryFilter !== "__uncategorized"
          ? categoryFilter
          : undefined,
      uncategorized:
        recordMode === "purchases" && categoryFilter === "__uncategorized" ? true : undefined,
      tagId: recordMode === "purchases" ? tagFilter || undefined : undefined,
      paymentAccountId: recordMode === "purchases" ? accountFilter || undefined : undefined,
      poolId: recordMode === "purchases" ? poolFilter || undefined : undefined,
      ...(recordMode === "purchases"
        ? toPurchaseAmountVariables({ minimumAmount, maximumAmount }, viewMode)
        : {}),
    });
  const purchaseBulkEdit = usePurchaseBulkEdit({
    householdId: household.id,
    purchases,
    refresh,
    selectionKey: JSON.stringify([
      household.id,
      recordMode,
      viewMode,
      search,
      fromMonth,
      toMonth,
      categoryFilter,
      poolFilter,
      accountFilter,
      tagFilter,
      originFilter,
      evidenceFilter,
      observationSourceFilter,
      observationStateFilter,
      minimumAmount,
      maximumAmount,
    ]),
  });
  const [observationCount, setObservationCount] = useState({ count: 0, totalCount: 0 });
  const updateObservationCount = useCallback((count: number, totalCount: number) => {
    setObservationCount((current) =>
      current.count === count && current.totalCount === totalCount
        ? current
        : { count, totalCount },
    );
  }, []);
  const [selectedItems, setSelectedItems] = useState<Set<string>>(() => new Set());
  const selectionAnchor = useRef<{ context: string; id: string } | null>(null);
  const [bulkCategory, setBulkCategory] = useState("");
  const [bulkAllocationsActive, setBulkAllocationsActive] = useState(false);
  const [bulkAllocations, setBulkAllocations] = useState<Record<string, string>>({});
  const [bulkVariantId, setBulkVariantId] = useState("");
  const [bulkTags, setBulkTags] = useState<string[]>([]);
  const [bulkTagsActive, setBulkTagsActive] = useState(false);
  const [tags, setTags] = useState<Tag[]>([]);
  const [tagsLoaded, setTagsLoaded] = useState(false);
  const [bulkPending, setBulkPending] = useState(false);
  const [bulkError, setBulkError] = useState("");
  const [bulkUndo, setBulkUndo] = useState<Array<{
    itemId: string;
    baseRevision: number;
    patch: Record<string, unknown>;
  }> | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const { evidence: purchaseEvidence, error: evidenceError } = usePurchaseEvidence(expanded);
  const [editing, setEditing] = useState<string | null>(null);
  const [purchaseDraft, setPurchaseDraft] = useState<PurchaseDraft | null>(null);
  const [itemDrafts, setItemDrafts] = useState<
    Record<
      string,
      {
        name: string;
        amountYen: string;
        quantity: string;
        quantityUnit: string;
        printedUnitPriceYen: string;
        printedBasisQuantity: string;
        printedUnitPriceUnit: string;
        categoryId: string;
        productVariantId: string;
        productLearning: "ITEM_ONLY" | "VARIANT_NAME" | "MERCHANT_ALIAS";
        variantName: string;
      }
    >
  >({});
  const [catalog, setCatalog] = useState<ProductConcept[]>([]);
  const [highlightedEvidenceItem, setHighlightedEvidenceItem] = useState<string | null>(null);
  const [editError, setEditError] = useState("");
  const [postingPurchase, setPostingPurchase] = useState<string | null>(null);
  const postDraftPurchase = async (purchase: Purchase) => {
    setPostingPurchase(purchase.id);
    setEditError("");
    try {
      await mutatePostPurchase({
        input: {
          purchaseId: purchase.id,
          expectedRevision: purchase.revision,
          idempotencyKey: makeIdempotencyKey(),
        },
      });
      await refresh();
    } catch (reason) {
      await refresh();
      setEditError((reason as Error).message);
    } finally {
      setPostingPurchase(null);
    }
  };
  useEffect(() => {
    setHighlightedEvidenceItem(null);
  }, [expanded]);
  useEffect(() => {
    let active = true;
    setTagsLoaded(false);
    void readTags(household.id)
      .then((result) => {
        if (!active) return;
        setTags(result);
        setTagsLoaded(true);
      })
      .catch((reason) => {
        if (!active) return;
        setBulkError((reason as Error).message);
        setTagsLoaded(true);
      });
    return () => {
      active = false;
    };
  }, [household.id]);
  useEffect(() => {
    const updates: Partial<LedgerSearch> = {};
    if (
      categoryFilter &&
      categoryFilter !== "__uncategorized" &&
      !household.categories.some((category) => category.id === categoryFilter)
    )
      updates.categoryFilter = "";
    if (poolFilter && !household.pools.some((pool) => pool.id === poolFilter))
      updates.poolFilter = "";
    if (accountFilter && !household.accounts.some((account) => account.id === accountFilter))
      updates.accountFilter = "";
    if (tagsLoaded && tagFilter && !tags.some((tag) => tag.id === tagFilter))
      updates.tagFilter = "";
    if (Object.keys(updates).length) onFiltersChange(updates);
  }, [
    accountFilter,
    categoryFilter,
    household.accounts,
    household.categories,
    household.pools,
    onFiltersChange,
    poolFilter,
    tagFilter,
    tags,
    tagsLoaded,
  ]);
  const [correctionTarget, setCorrectionTarget] = useState<{
    purchaseId: string;
    itemId: string;
  } | null>(null);
  const [conflict, setConflict] = useState<{
    label: string;
    localValue: string;
    serverValue: string;
    retry: () => Promise<void>;
    discard: () => Promise<void>;
  } | null>(null);
  useEffect(() => {
    setSelectedItems(new Set());
    selectionAnchor.current = null;
    setBulkUndo(null);
    setBulkError("");
  }, [household.id]);
  useEffect(() => {
    setSelectedItems(new Set());
    selectionAnchor.current = null;
  }, [
    recordMode,
    viewMode,
    search,
    fromMonth,
    toMonth,
    categoryFilter,
    poolFilter,
    accountFilter,
    tagFilter,
    originFilter,
    evidenceFilter,
    observationSourceFilter,
    observationStateFilter,
    minimumAmount,
    maximumAmount,
  ]);
  const latestPurchase = async (purchaseId: string) => {
    let after: string | null = null;
    for (;;) {
      const result: { purchases: Connection<Purchase> } = await readGraphql(purchasesQuery, {
        filter: { householdId: household.id },
        first: 100,
        after,
      });
      const match = result.purchases.edges.find((edge) => edge.node.id === purchaseId)?.node;
      if (match) return match;
      const page: Connection<Purchase>["pageInfo"] = result.purchases.pageInfo;
      if (!page.hasNextPage || !page.endCursor || page.endCursor === after) return undefined;
      after = page.endCursor;
    }
  };
  useEffect(() => {
    let active = true;
    void readProductCatalog(household.id)
      .then((products) => active && setCatalog(products))
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [household.id]);
  const startEditing = (purchase: Purchase) => {
    setEditing(purchase.id);
    setPurchaseDraft(createPurchaseDraft(purchase));
    setItemDrafts(
      Object.fromEntries(
        purchase.items.map((item) => [
          item.id,
          {
            name: item.name,
            amountYen: String(item.amountYen),
            quantity: formatQuantity(item.quantityNumerator, item.quantityDenominator),
            quantityUnit: item.quantityUnit ?? "",
            printedUnitPriceYen:
              item.printedUnitPriceYen == null ? "" : String(item.printedUnitPriceYen),
            printedBasisQuantity: formatQuantity(
              item.printedUnitPriceQuantityNumerator,
              item.printedUnitPriceQuantityDenominator,
            ),
            printedUnitPriceUnit: item.printedUnitPriceUnit ?? "",
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
  };
  const saveItem = async (purchase: Purchase, item: PurchaseItem) => {
    const draft = itemDrafts[item.id];
    if (!draft || !draft.name.trim() || Number(draft.amountYen) < 1) return;
    let quantity;
    let printedBasis;
    try {
      quantity = parseQuantity(draft.quantity, draft.quantityUnit);
      printedBasis = parseQuantity(draft.printedBasisQuantity, draft.printedUnitPriceUnit);
    } catch (reason) {
      setEditError((reason as Error).message);
      return;
    }
    const printedPriceText = draft.printedUnitPriceYen.trim();
    const hasPrintedPrice = Boolean(
      printedPriceText || draft.printedBasisQuantity.trim() || draft.printedUnitPriceUnit.trim(),
    );
    const printedPrice = printedPriceText ? Number(printedPriceText.replace(/\D/g, "")) : null;
    if (
      hasPrintedPrice &&
      (!printedBasis || printedPrice === null || !Number.isSafeInteger(printedPrice))
    ) {
      setEditError("印字単価、基準量、単位を確認してください");
      return;
    }
    const patch = {
      name: draft.name.trim(),
      amountYen: Number(draft.amountYen),
      categoryId: draft.categoryId || null,
      productVariantId: draft.productVariantId || null,
    };
    const baseChangedFields = [
      ...(patch.name !== item.name ? ["name"] : []),
      ...(patch.amountYen !== item.amountYen ? ["amountYen"] : []),
      ...(!sameNullableString(patch.categoryId, item.categoryId) ? ["categoryId"] : []),
      ...(!sameNullableString(patch.productVariantId, item.productVariantId)
        ? ["productVariantId"]
        : []),
    ] as Array<keyof typeof patch>;
    const quantityChanged =
      !sameNullableString(quantity?.numerator, item.quantityNumerator) ||
      !sameNullableString(quantity?.denominator, item.quantityDenominator) ||
      !sameNullableString(quantity?.unit, item.quantityUnit);
    const printedPriceChanged =
      (printedPrice ?? null) !== (item.printedUnitPriceYen ?? null) ||
      !sameNullableString(printedBasis?.numerator, item.printedUnitPriceQuantityNumerator) ||
      !sameNullableString(printedBasis?.denominator, item.printedUnitPriceQuantityDenominator) ||
      !sameNullableString(printedBasis?.unit, item.printedUnitPriceUnit);
    const changedFields = [
      ...baseChangedFields,
      ...(quantityChanged ? ["quantity"] : []),
      ...(printedPriceChanged ? ["printedUnitPrice"] : []),
    ];
    if (!changedFields.length) return;
    const changedPatch = Object.fromEntries(
      baseChangedFields.map((field) => [field, patch[field]]),
    );
    const inputForRevision = (baseRevision: number) =>
      mapUpdatePurchaseItemInput({
        itemId: item.id,
        baseRevision,
        ...changedPatch,
        ...(quantityChanged
          ? {
              quantityNumerator: quantity?.numerator ?? null,
              quantityDenominator: quantity?.denominator ?? null,
              quantityUnit: quantity?.unit ?? null,
            }
          : {}),
        ...(printedPriceChanged
          ? {
              printedUnitPriceYen: hasPrintedPrice ? printedPrice : null,
              printedUnitPriceQuantityNumerator: printedBasis?.numerator ?? null,
              printedUnitPriceQuantityDenominator: printedBasis?.denominator ?? null,
              printedUnitPriceUnit: printedBasis?.unit ?? null,
            }
          : {}),
        ...(baseChangedFields.includes("productVariantId") && patch.productVariantId
          ? {
              productLearning:
                draft.productLearning === "VARIANT_NAME"
                  ? { variantName: draft.variantName.trim() }
                  : draft.productLearning === "MERCHANT_ALIAS"
                    ? { merchantAlias: true }
                    : { itemOnly: true },
            }
          : {}),
      });
    try {
      await mutateUpdatePurchaseItem({
        input: inputForRevision(item.revision),
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
              await mutateUpdatePurchaseItem({
                input: inputForRevision(current.revision),
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
    const currentAllocations = purchase.paymentAllocations.map((payment) => ({
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
      await mutateUpdatePurchase({
        input: {
          purchaseId: purchase.id,
          baseRevision: purchase.revision,
          patch: changedPatch,
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
              await mutateUpdatePurchase({
                input: {
                  purchaseId: latest.id,
                  baseRevision: latest.revision,
                  patch: changedPatch,
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
  const filtered = purchases;
  const filteredItems = purchases.flatMap((purchase) =>
    purchase.items.map((item) => ({ purchase, item })),
  );
  const itemCoverageComplete = !loadingMore && !hasNextPage && !moreError;
  const selectedCount = selectedItems.size;
  const bulkAllocationEntries = Object.entries(bulkAllocations)
    .filter(([, weight]) => Number(weight) > 0)
    .map(([poolId, weight]) => ({ poolId, weight: Number(weight) }));
  const toggleSelection = (
    context: string,
    visibleIds: string[],
    id: string,
    shiftKey: boolean,
  ) => {
    const current = selectedItems;
    const anchorIndex =
      shiftKey && selectionAnchor.current?.context === context
        ? visibleIds.indexOf(selectionAnchor.current.id)
        : -1;
    const targetIndex = visibleIds.indexOf(id);
    const range =
      anchorIndex >= 0 && targetIndex >= 0
        ? visibleIds.slice(
            Math.min(anchorIndex, targetIndex),
            Math.max(anchorIndex, targetIndex) + 1,
          )
        : null;
    const additions = range?.filter((targetId) => !current.has(targetId)) ?? [];
    if (current.size + additions.length > 100) {
      setBulkError("一括変更は100品目までです");
      return;
    }
    const next = new Set(current);
    if (range) range.forEach((targetId) => next.add(targetId));
    else if (next.has(id)) next.delete(id);
    else next.add(id);
    if (!shiftKey) selectionAnchor.current = { context, id };
    setBulkError("");
    setSelectedItems(next);
  };
  const applyBulkEdit = async () => {
    if (!bulkCategory && !bulkAllocationsActive && !bulkVariantId && !bulkTagsActive) return;
    const targets = purchases.flatMap((purchase) =>
      purchase.items
        .filter((item) => selectedItems.has(item.id))
        .map((item) => ({ purchase, item })),
    );
    if (!targets.length) return;
    setBulkPending(true);
    setBulkError("");
    setBulkUndo(null);
    try {
      const patch: Record<string, unknown> = {};
      const changedFields: string[] = [];
      if (bulkCategory) {
        patch.categoryId = bulkCategory === "__uncategorized" ? null : bulkCategory;
        changedFields.push("categoryId");
      }
      if (bulkAllocationsActive) {
        patch.allocations = bulkAllocationEntries;
        changedFields.push("allocations");
      }
      if (bulkVariantId) {
        patch.productVariantId = bulkVariantId === "__none" ? null : bulkVariantId;
        changedFields.push("productVariantId");
      }
      if (bulkTagsActive) {
        patch.tagIds = bulkTags;
        changedFields.push("tags");
      }
      const result = await mutateBulkUpdatePurchaseItems({
        input: {
          targets: targets.map(({ item }) => ({ itemId: item.id, baseRevision: item.revision })),
          patch,
        },
      });
      const undoEntries: NonNullable<typeof bulkUndo> = targets.map(({ item }) => {
        const undoPatch: Record<string, unknown> = {};
        if (bulkCategory) undoPatch.categoryId = item.categoryId ?? null;
        if (bulkAllocationsActive)
          undoPatch.allocations = item.allocations
            .filter((allocation) => allocation.weight > 0)
            .map(({ poolId, weight }) => ({ poolId, weight }));
        if (bulkVariantId) undoPatch.productVariantId = item.productVariantId ?? null;
        if (bulkTagsActive) undoPatch.tagIds = item.tags.map((tag) => tag.id);
        return {
          itemId: item.id,
          baseRevision:
            result.bulkUpdatePurchaseItems.find((updated) => updated.id === item.id)?.revision ??
            item.revision + 1,
          patch: undoPatch,
        };
      });
      setBulkUndo(undoEntries.length ? undoEntries : null);
      setSelectedItems(new Set());
      selectionAnchor.current = null;
      setBulkCategory("");
      setBulkAllocationsActive(false);
      setBulkAllocations({});
      setBulkVariantId("");
      setBulkTags([]);
      setBulkTagsActive(false);
      await refresh();
    } catch (reason) {
      setBulkError((reason as Error).message);
      await refresh();
    } finally {
      setBulkPending(false);
    }
  };
  const undoBulkEdit = async () => {
    if (!bulkUndo?.length) return;
    setBulkPending(true);
    setBulkError("");
    let undone = 0;
    try {
      const groups = new Map<string, NonNullable<typeof bulkUndo>>();
      for (const entry of bulkUndo) {
        const key = JSON.stringify(entry.patch);
        groups.set(key, [...(groups.get(key) ?? []), entry]);
      }
      for (const group of groups.values()) {
        if (!group.length) continue;
        await mutateBulkUpdatePurchaseItems({
          input: {
            targets: group.map(({ itemId, baseRevision }) => ({ itemId, baseRevision })),
            patch: group[0].patch,
          },
        });
        undone += group.length;
      }
      setBulkUndo(null);
      await refresh();
    } catch (reason) {
      setBulkError((reason as Error).message);
      setBulkUndo(bulkUndo.slice(undone));
      await refresh();
    } finally {
      setBulkPending(false);
    }
  };
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
      <div className={styles.controls}>
        {recordMode === "purchases" && (
          <label className="input-with-icon">
            <Search size={16} />
            <input
              value={search}
              onChange={(e) => updateFilters({ search: e.target.value })}
              placeholder="店名、品名で検索"
            />
          </label>
        )}
        <label className="select-filter">
          <span>開始</span>
          <input
            aria-label="開始月"
            type="month"
            value={fromMonth}
            onChange={(event) => updateFilters({ fromMonth: event.target.value })}
          />
        </label>
        <label className="select-filter">
          <span>終了</span>
          <input
            aria-label="終了月"
            type="month"
            value={toMonth}
            onChange={(event) => updateFilters({ toMonth: event.target.value })}
          />
        </label>
        <div className="record-count">
          {recordMode === "observations"
            ? `${observationCount.count}/${observationCount.totalCount}件表示`
            : viewMode === "items"
              ? `${filteredItems.length}品目表示 · ${purchases.length}/${totalCount}件購入取得`
              : `${filtered.length}件表示 · ${purchases.length}/${totalCount}件読込`}
        </div>
      </div>
      <div className={styles.filters}>
        <label className="select-filter">
          <span>表示</span>
          <select
            value={recordMode}
            onChange={(event) => {
              const mode = event.target.value as "purchases" | "observations";
              updateFilters({ recordMode: mode });
              if (mode === "observations") {
                setSelectedItems(new Set());
                selectionAnchor.current = null;
              }
            }}
          >
            <option value="purchases">購入履歴</option>
            <option value="observations">照合候補</option>
          </select>
        </label>
        {recordMode === "purchases" && (
          <label className="select-filter">
            <span>単位</span>
            <select
              value={viewMode}
              onChange={(event) => {
                updateFilters({ viewMode: event.target.value as "purchases" | "items" });
                setSelectedItems(new Set());
                selectionAnchor.current = null;
              }}
            >
              <option value="purchases">購入</option>
              <option value="items">品目</option>
            </select>
          </label>
        )}
        {recordMode === "purchases" ? (
          <>
            <label className="select-filter">
              <span>入力元</span>
              <select
                value={originFilter}
                onChange={(event) =>
                  updateFilters({ originFilter: event.target.value as Purchase["origin"] | "" })
                }
              >
                <option value="">すべて</option>
                <option value="AI">AI入力</option>
                <option value="MANUAL">手入力</option>
                <option value="UNKNOWN">不明</option>
              </select>
            </label>
            <label className="select-filter">
              <span>原本</span>
              <select
                value={evidenceFilter}
                onChange={(event) =>
                  updateFilters({ evidenceFilter: event.target.value as "true" | "false" | "" })
                }
              >
                <option value="">すべて</option>
                <option value="true">あり</option>
                <option value="false">なし</option>
              </select>
            </label>
            <label className="select-filter">
              <span>カテゴリ</span>
              <select
                value={categoryFilter}
                onChange={(event) => updateFilters({ categoryFilter: event.target.value })}
              >
                <option value="">すべて</option>
                <option value="__uncategorized">未分類</option>
                {household.categories.map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="select-filter">
              <span>負担先</span>
              <select
                value={poolFilter}
                onChange={(event) => updateFilters({ poolFilter: event.target.value })}
              >
                <option value="">すべて</option>
                {household.pools.map((pool) => (
                  <option key={pool.id} value={pool.id}>
                    {pool.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="select-filter">
              <span>決済元</span>
              <select
                value={accountFilter}
                onChange={(event) => updateFilters({ accountFilter: event.target.value })}
              >
                <option value="">すべて</option>
                {household.accounts.map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="select-filter">
              <span>タグ</span>
              <select
                value={tagFilter}
                onChange={(event) => updateFilters({ tagFilter: event.target.value })}
              >
                <option value="">すべて</option>
                {tags
                  .filter((tag) => !tag.archived)
                  .map((tag) => (
                    <option key={tag.id} value={tag.id}>
                      {tag.name}
                    </option>
                  ))}
              </select>
            </label>
            <label className="select-filter">
              <span>{viewMode === "items" ? "品目額" : "購入額"}</span>
              <input
                aria-label="最低金額"
                type="number"
                min="0"
                placeholder="下限"
                value={minimumAmount}
                onChange={(event) => updateFilters({ minimumAmount: event.target.value })}
              />
              <span>〜</span>
              <input
                aria-label="最高金額"
                type="number"
                min="0"
                placeholder="上限"
                value={maximumAmount}
                onChange={(event) => updateFilters({ maximumAmount: event.target.value })}
              />
            </label>
          </>
        ) : (
          <>
            <label className="select-filter">
              <span>状態</span>
              <select
                value={observationStateFilter}
                onChange={(event) =>
                  updateFilters({
                    observationStateFilter: event.target.value as UnmatchedObservationState | "",
                  })
                }
              >
                <option value="UNMATCHED">未照合</option>
                <option value="CONFIRMED">照合済み</option>
                <option value="">すべて</option>
              </select>
            </label>
            <label className="select-filter">
              <span>入力元</span>
              <select
                value={observationSourceFilter}
                onChange={(event) =>
                  updateFilters({ observationSourceFilter: event.target.value as SourceKind | "" })
                }
              >
                <option value="">すべて</option>
                <option value="RECEIPT">レシート</option>
                <option value="ORDER">注文</option>
                <option value="EMAIL">メール</option>
                <option value="CARD">カード</option>
                <option value="BANK">銀行</option>
              </select>
            </label>
          </>
        )}
      </div>
      <PurchaseBulkActions
        accounts={household.accounts}
        bulkEdit={purchaseBulkEdit}
        showSelectionControls={recordMode === "purchases" && viewMode === "purchases"}
      />
      {selectedCount > 0 && (
        <section
          className={`panel ledger-bulk-actions ${styles.bulkActions}`}
          aria-label="選択品目の一括編集"
        >
          <span>{selectedCount}品目を選択中</span>
          <label className="select-filter">
            <span>カテゴリ</span>
            <select
              value={bulkCategory}
              disabled={bulkPending || editing !== null || Boolean(correctionTarget)}
              onChange={(event) => setBulkCategory(event.target.value)}
            >
              <option value="">変更なし</option>
              <option value="__uncategorized">未分類</option>
              {household.categories
                .filter((category) => !category.archived)
                .map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
            </select>
          </label>
          <fieldset
            className="ledger-bulk-allocations"
            disabled={bulkPending || editing !== null || Boolean(correctionTarget)}
          >
            <legend>負担先を置換</legend>
            <label>
              <input
                type="checkbox"
                checked={bulkAllocationsActive}
                onChange={(event) => setBulkAllocationsActive(event.target.checked)}
              />
              {bulkAllocationsActive ? "置換" : "変更なし"}
            </label>
            {bulkAllocationsActive &&
              household.pools.map((pool) => (
                <label key={pool.id}>
                  <input
                    type="checkbox"
                    checked={bulkAllocations[pool.id] !== undefined}
                    onChange={(event) =>
                      setBulkAllocations((current) => {
                        const next = { ...current };
                        if (event.target.checked) next[pool.id] = "1";
                        else delete next[pool.id];
                        return next;
                      })
                    }
                  />
                  {pool.name}
                  {bulkAllocations[pool.id] !== undefined && (
                    <input
                      aria-label={`${pool.name}の負担比率`}
                      inputMode="decimal"
                      type="number"
                      min="0.01"
                      step="0.01"
                      value={bulkAllocations[pool.id]}
                      onChange={(event) =>
                        setBulkAllocations((current) => ({
                          ...current,
                          [pool.id]: event.target.value,
                        }))
                      }
                    />
                  )}
                </label>
              ))}
          </fieldset>
          <label className="select-filter">
            <span>商品</span>
            <select
              value={bulkVariantId}
              disabled={bulkPending || editing !== null || Boolean(correctionTarget)}
              onChange={(event) => setBulkVariantId(event.target.value)}
            >
              <option value="">変更なし</option>
              {catalog.flatMap((concept) =>
                concept.variants
                  .filter((variant) => !variant.archived)
                  .map((variant) => (
                    <option key={variant.id} value={variant.id}>
                      {concept.name} · {variant.name}
                    </option>
                  )),
              )}
              <option value="__none">商品を解除</option>
            </select>
          </label>
          <label className="select-filter">
            <span>タグを置換</span>
            <input
              type="checkbox"
              checked={bulkTagsActive}
              disabled={bulkPending || editing !== null || Boolean(correctionTarget)}
              onChange={(event) => setBulkTagsActive(event.target.checked)}
            />
            <select
              aria-label="一括設定するタグ"
              multiple
              value={bulkTags}
              disabled={
                !bulkTagsActive || bulkPending || editing !== null || Boolean(correctionTarget)
              }
              onChange={(event) =>
                setBulkTags(
                  Array.from(event.currentTarget.selectedOptions, (option) => option.value),
                )
              }
            >
              {tags
                .filter((tag) => !tag.archived)
                .map((tag) => (
                  <option key={tag.id} value={tag.id}>
                    {tag.name}
                  </option>
                ))}
            </select>
          </label>
          <button
            className="button button-primary"
            type="button"
            disabled={
              bulkPending ||
              editing !== null ||
              Boolean(correctionTarget) ||
              (!bulkCategory && !bulkAllocationsActive && !bulkVariantId && !bulkTagsActive) ||
              (bulkAllocationsActive &&
                Object.values(bulkAllocations).some(
                  (weight) => !Number.isFinite(Number(weight)) || Number(weight) <= 0,
                ))
            }
            onClick={() => void applyBulkEdit()}
          >
            {bulkPending ? "更新中" : "一括変更"}
          </button>
          <button
            className="text-button"
            type="button"
            disabled={bulkPending}
            onClick={() => {
              setSelectedItems(new Set());
              selectionAnchor.current = null;
            }}
          >
            選択解除
          </button>
        </section>
      )}
      {bulkUndo && (
        <section className={`panel ledger-bulk-actions ${styles.bulkActions}`} aria-live="polite">
          <span>{bulkUndo.length}品目の変更</span>
          <button
            className="button button-soft"
            type="button"
            disabled={bulkPending}
            onClick={() => void undoBulkEdit()}
          >
            元に戻す
          </button>
        </section>
      )}
      {bulkError && <InlineError message={bulkError} />}
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
      {recordMode === "observations" && (
        <ObservationLedger
          household={household}
          filters={{ fromMonth, toMonth, observationSourceFilter, observationStateFilter }}
          onUnsavedChanges={onUnsavedChanges}
          onCountChange={updateObservationCount}
        />
      )}
      {recordMode === "purchases" && viewMode === "items" && (
        <section className="panel ledger-list">
          {filteredItems.length === 0 && itemCoverageComplete ? (
            <EmptyState
              icon={<BookOpen />}
              title="品目が見つかりません"
              action="買い物を追加"
              onClick={onAdd}
            />
          ) : filteredItems.length === 0 ? (
            <div className="skeleton-list">
              <i />
              <i />
              <i />
            </div>
          ) : (
            filteredItems.map(({ purchase, item }) => (
              <div className="ledger-entry" key={item.id}>
                <div className="ledger-item-row">
                  <input
                    className="item-select"
                    type="checkbox"
                    aria-label={`${item.name}を選択`}
                    checked={selectedItems.has(item.id)}
                    onChange={(event) =>
                      toggleSelection(
                        "items",
                        filteredItems.map(({ item: visibleItem }) => visibleItem.id),
                        item.id,
                        event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey,
                      )
                    }
                    disabled={bulkPending}
                  />
                  <div className="purchase-description">
                    <b>{item.name}</b>
                    <span>
                      {purchase.merchant || "店舗未入力"} · {purchaseMonth(purchase.occurredAt)} ·{" "}
                      {household.categories.find((category) => category.id === item.categoryId)
                        ?.name ?? "未分類"}
                      {item.allocations.length > 0 &&
                        ` · ${item.allocations.map((allocation) => allocation.poolName).join("・")}`}
                      {item.tags.length > 0 && ` · ${item.tags.map((tag) => tag.name).join("・")}`}
                    </span>
                  </div>
                  <div className="purchase-meta">
                    <b>{yen(item.amountYen)}</b>
                    <span>購入 {yen(purchase.totalYen)}</span>
                  </div>
                  <button
                    className="text-button"
                    type="button"
                    onClick={() => {
                      updateFilters({ recordMode: "purchases", viewMode: "purchases" });
                      setExpanded(purchase.id);
                    }}
                  >
                    購入を開く
                  </button>
                </div>
              </div>
            ))
          )}
          {loadingMore && <div className={styles.more}>購入履歴を取得中</div>}
          {moreError && <InlineError message={moreError} />}
          {moreError && hasNextPage && (
            <div className={styles.more}>
              <button
                className="button button-soft"
                disabled={loadingMore}
                onClick={() => void loadMore()}
              >
                {loadingMore ? "読み込み中" : "取得を再試行"}
              </button>
            </div>
          )}
        </section>
      )}
      {recordMode === "purchases" && viewMode === "purchases" && (
        <section className="panel ledger-list">
          {filtersActive && loadingMore ? (
            <div className="skeleton-list">
              <i />
              <i />
              <i />
            </div>
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
                <label>
                  <input
                    type="checkbox"
                    aria-label={`${purchase.merchant || "店舗未入力"}の購入を選択`}
                    checked={purchaseBulkEdit.selectedPurchases.has(purchase.id)}
                    disabled={purchaseBulkEdit.pending || purchase.status === "CLOSED"}
                    onChange={(event) =>
                      purchaseBulkEdit.selectPurchase(
                        filtered.map((visiblePurchase) => visiblePurchase.id),
                        purchase.id,
                        event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey,
                      )
                    }
                  />
                  選択
                </label>
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
                                )?.name ??
                                "決済元";
                              return `${accountName} ${yen(payment.amountYen)}`;
                            })
                            .join(" / ")
                        : "決済元未設定"}
                    </span>
                  </div>
                  <div className="purchase-meta">
                    <b>{yen(purchase.totalYen)}</b>
                    <span>
                      {purchase.status === "CLOSED"
                        ? "締め済み"
                        : purchase.status === "DRAFT"
                          ? "下書き"
                          : "記録済み"}{" "}
                      ·{" "}
                      {purchase.origin === "AI"
                        ? "AI入力"
                        : purchase.origin === "MANUAL"
                          ? "手入力"
                          : "入力元不明"}
                      {purchase.hasEvidence ? " · 原本あり" : " · 原本なし"}
                    </span>
                  </div>
                  {expanded === purchase.id ? (
                    <ChevronDown size={16} />
                  ) : (
                    <ChevronRight size={16} />
                  )}
                </button>
                {expanded === purchase.id && (
                  <div className="expanded-items">
                    {purchase.status === "DRAFT" && (
                      <section className="panel ledger-evidence-panel">
                        <div className="panel-heading">
                          <div>
                            <span className="eyebrow">下書き</span>
                          </div>
                          <button
                            className="button button-primary"
                            type="button"
                            disabled={postingPurchase === purchase.id}
                            onClick={() => void postDraftPurchase(purchase)}
                          >
                            {postingPurchase === purchase.id ? "記録しています" : "支出に記録"}
                          </button>
                        </div>
                      </section>
                    )}
                    <PurchaseEvidence
                      purchase={purchase}
                      evidence={purchaseEvidence}
                      error={evidenceError}
                      highlightedItemId={highlightedEvidenceItem}
                      onHighlightItem={setHighlightedEvidenceItem}
                    />
                    {editing === purchase.id && purchaseDraft ? (
                      <PurchaseEditForm
                        purchase={purchase}
                        draft={purchaseDraft}
                        accounts={household.accounts}
                        onChange={setPurchaseDraft}
                        onCancel={() => setEditing(null)}
                        onSave={(target) => void savePurchase(target)}
                      />
                    ) : (
                      <button className="text-button" onClick={() => startEditing(purchase)}>
                        買い物を編集
                      </button>
                    )}
                    {(purchase.corrections ?? []).map((correction) => (
                      <div className="purchase-correction-entry" key={correction.id}>
                        <span>
                          {correction.__typename === "PurchaseItemCorrection"
                            ? "品目返品"
                            : "購入訂正"}
                        </span>
                        <b
                          className={
                            correction.amountDeltaYen < 0 ? "settlement-credit" : undefined
                          }
                        >
                          {yen(correction.amountDeltaYen)}
                        </b>
                        <small>{correction.reason}</small>
                      </div>
                    ))}
                    {purchase.items.map((item) => (
                      <div
                        className={`expanded-item ${highlightedEvidenceItem === item.id ? "evidence-item-highlighted" : ""}`}
                        key={item.id}
                      >
                        <input
                          className="item-select"
                          type="checkbox"
                          aria-label={`${item.name}を選択`}
                          checked={selectedItems.has(item.id)}
                          onChange={(event) =>
                            toggleSelection(
                              `purchase:${purchase.id}`,
                              purchase.items.map((visibleItem) => visibleItem.id),
                              item.id,
                              event.nativeEvent instanceof MouseEvent && event.nativeEvent.shiftKey,
                            )
                          }
                          disabled={editing === purchase.id || bulkPending}
                        />
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
                                  [item.id]: {
                                    ...current[item.id],
                                    categoryId: event.target.value,
                                  },
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
                                        .find((variant) => variant.id === event.target.value)
                                        ?.name ?? "",
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
                            <details className="item-measure-details">
                              <summary>数量・印字単価</summary>
                              <label>
                                数量
                                <input
                                  value={itemDrafts[item.id]?.quantity ?? ""}
                                  onChange={(event) =>
                                    setItemDrafts((current) => ({
                                      ...current,
                                      [item.id]: {
                                        ...current[item.id],
                                        quantity: event.target.value,
                                      },
                                    }))
                                  }
                                  placeholder="1、1/2"
                                />
                              </label>
                              <label>
                                単位
                                <input
                                  value={itemDrafts[item.id]?.quantityUnit ?? ""}
                                  onChange={(event) =>
                                    setItemDrafts((current) => ({
                                      ...current,
                                      [item.id]: {
                                        ...current[item.id],
                                        quantityUnit: event.target.value,
                                      },
                                    }))
                                  }
                                  maxLength={32}
                                />
                              </label>
                              <label>
                                印字単価
                                <input
                                  inputMode="numeric"
                                  value={itemDrafts[item.id]?.printedUnitPriceYen ?? ""}
                                  onChange={(event) =>
                                    setItemDrafts((current) => ({
                                      ...current,
                                      [item.id]: {
                                        ...current[item.id],
                                        printedUnitPriceYen: event.target.value.replace(/\D/g, ""),
                                      },
                                    }))
                                  }
                                />
                              </label>
                              <label>
                                基準量
                                <input
                                  value={itemDrafts[item.id]?.printedBasisQuantity ?? ""}
                                  onChange={(event) =>
                                    setItemDrafts((current) => ({
                                      ...current,
                                      [item.id]: {
                                        ...current[item.id],
                                        printedBasisQuantity: event.target.value,
                                      },
                                    }))
                                  }
                                  placeholder="1、100、1/2"
                                />
                              </label>
                              <label>
                                印字単位
                                <input
                                  value={itemDrafts[item.id]?.printedUnitPriceUnit ?? ""}
                                  onChange={(event) =>
                                    setItemDrafts((current) => ({
                                      ...current,
                                      [item.id]: {
                                        ...current[item.id],
                                        printedUnitPriceUnit: event.target.value,
                                      },
                                    }))
                                  }
                                  maxLength={32}
                                />
                              </label>
                            </details>
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
                                .map((allocation) => `${allocation.poolName} ${allocation.weight}`)
                                .join(" / ")}
                            </span>
                            {item.tags.length > 0 && (
                              <span className="item-tag-list">
                                {item.tags.map((tag) => (
                                  <i key={tag.id}>{tag.name}</i>
                                ))}
                              </span>
                            )}
                          </>
                        )}
                        {editing !== purchase.id &&
                          (correctionTarget?.purchaseId === purchase.id &&
                          correctionTarget.itemId === item.id ? (
                            <PurchaseCorrection
                              householdId={household.id}
                              accounts={household.accounts}
                              purchase={purchase}
                              item={item}
                              onCancel={() => setCorrectionTarget(null)}
                              onSaved={async () => {
                                setCorrectionTarget(null);
                                await refresh();
                              }}
                            />
                          ) : (
                            <button
                              className="text-button correction-trigger"
                              type="button"
                              disabled={getRemainingRefundAmount(purchase, item) <= 0}
                              onClick={() => startCorrection(purchase, item)}
                            >
                              返品を記録
                            </button>
                          ))}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ))
          )}
        </section>
      )}
      {recordMode === "purchases" && moreError && <InlineError message={moreError} />}
      {recordMode === "purchases" && hasNextPage && !moreError && (
        <div className={styles.more}>
          <button
            className="button button-soft"
            disabled={loadingMore}
            onClick={() => void loadMore()}
          >
            {loadingMore
              ? "読み込み中"
              : filtersActive
                ? "検索結果をさらに読み込む"
                : "古い記録を読み込む"}
          </button>
        </div>
      )}
      {recordMode === "purchases" && viewMode === "purchases" && moreError && hasNextPage && (
        <div className={styles.more}>
          <button
            className="button button-soft"
            disabled={loadingMore}
            onClick={() => void loadMore()}
          >
            {loadingMore ? "読み込み中" : "取得を再試行"}
          </button>
        </div>
      )}
    </>
  );
}

import { useEffect, useRef, useState } from "react";
import { BookOpen, ChevronDown, ChevronRight, Plus, Search, X } from "lucide-react";
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
  bulkUpdatePurchasesMutation,
  correctPurchaseMutation,
  postPurchaseMutation,
  purchasesQuery,
  updatePurchaseItemMutation,
  updatePurchaseMutation,
  useHouseholdPurchases,
} from "../../../entities/purchase";
import { ledgerQuery } from "../../../entities/ledger";
import { unmatchedObservationsQuery } from "../api/observation/operations";
import { readProductCatalog } from "../../../entities/product";
import { tagsQuery } from "../../../entities/tag";
import { EmptyState, InlineError } from "../../../shared/ui";
import { formatQuantity, parseQuantity } from "../../../shared/lib";
import { useUnmatchedObservations } from "../api/observation/use-unmatched-observations";
import { ObservationLinker } from "./observation-linker";

type PurchaseEvidence = {
  documentId: string;
  itemId?: string | null;
  sourceKind: string;
  mimeType: string;
  region?: { page: number; top: number; left: number; bottom: number; right: number } | null;
};

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

export function LedgerPage({ household, onAdd }: { household: Household; onAdd: () => void }) {
  const { mutate: mutatePostPurchase } = useGraphqlMutation(postPurchaseMutation);
  const { mutate: mutateCorrectPurchase } = useGraphqlMutation(correctPurchaseMutation);
  const { mutate: mutateUpdatePurchaseItem } = useGraphqlMutation(updatePurchaseItemMutation);
  const { mutate: mutateUpdatePurchase } = useGraphqlMutation(updatePurchaseMutation);
  const { mutate: mutateBulkUpdatePurchases } = useGraphqlMutation<{
    bulkUpdatePurchases: { purchases: Array<{ id: string; revision: number }> };
  }>(bulkUpdatePurchasesMutation);
  const { mutate: mutateBulkUpdatePurchaseItems } = useGraphqlMutation<{
    bulkUpdatePurchaseItems: Array<{ id: string; revision: number }>;
  }>(bulkUpdatePurchaseItemsMutation);
  const [recordMode, setRecordMode] = useState<"purchases" | "observations">("purchases");
  const [viewMode, setViewMode] = useState<"purchases" | "items">("purchases");
  const [search, setSearch] = useState("");
  const [fromMonth, setFromMonth] = useState("");
  const [toMonth, setToMonth] = useState("");
  const [categoryFilter, setCategoryFilter] = useState("");
  const [poolFilter, setPoolFilter] = useState("");
  const [accountFilter, setAccountFilter] = useState("");
  const [tagFilter, setTagFilter] = useState("");
  const [originFilter, setOriginFilter] = useState<Purchase["origin"] | "">("");
  const [evidenceFilter, setEvidenceFilter] = useState("");
  const [observationSourceFilter, setObservationSourceFilter] = useState<SourceKind | "">("");
  const [observationStateFilter, setObservationStateFilter] = useState<
    UnmatchedObservationState | ""
  >("UNMATCHED");
  const [minimumAmount, setMinimumAmount] = useState("");
  const [maximumAmount, setMaximumAmount] = useState("");
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
      loadAll: recordMode === "purchases" && (viewMode === "items" || filtersActive),
    });
  const unmatched = useUnmatchedObservations(household.id, {
    enabled: recordMode === "observations",
    from: fromMonth || undefined,
    to: toMonth || undefined,
    sourceKind: observationSourceFilter || undefined,
    state: observationStateFilter || undefined,
  });
  const [selectedItems, setSelectedItems] = useState<Set<string>>(() => new Set());
  const [selectedPurchases, setSelectedPurchases] = useState<Set<string>>(() => new Set());
  const selectionAnchors = useRef<{
    item: { context: string; id: string } | null;
    purchase: { context: string; id: string } | null;
  }>({ item: null, purchase: null });
  const [bulkMerchantActive, setBulkMerchantActive] = useState(false);
  const [bulkMerchant, setBulkMerchant] = useState("");
  const [bulkOccurredAtActive, setBulkOccurredAtActive] = useState(false);
  const [bulkOccurredAt, setBulkOccurredAt] = useState("");
  const [bulkPaymentAccountId, setBulkPaymentAccountId] = useState("");
  const [purchaseBulkUndo, setPurchaseBulkUndo] = useState<Array<{
    purchaseId: string;
    baseRevision: number;
    patch: Record<string, unknown>;
  }> | null>(null);
  const [bulkCategory, setBulkCategory] = useState("");
  const [bulkAllocationsActive, setBulkAllocationsActive] = useState(false);
  const [bulkAllocations, setBulkAllocations] = useState<Record<string, string>>({});
  const [bulkVariantId, setBulkVariantId] = useState("");
  const [bulkTags, setBulkTags] = useState<string[]>([]);
  const [bulkTagsActive, setBulkTagsActive] = useState(false);
  const [tags, setTags] = useState<Tag[]>([]);
  const [bulkPending, setBulkPending] = useState(false);
  const [bulkError, setBulkError] = useState("");
  const [bulkUndo, setBulkUndo] = useState<Array<{
    itemId: string;
    baseRevision: number;
    patch: Record<string, unknown>;
  }> | null>(null);
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
  const [purchaseEvidence, setPurchaseEvidence] = useState<PurchaseEvidence[]>([]);
  const [evidenceError, setEvidenceError] = useState("");
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
    if (!expanded) {
      setPurchaseEvidence([]);
      setEvidenceError("");
      setHighlightedEvidenceItem(null);
      return;
    }
    let active = true;
    setPurchaseEvidence([]);
    setEvidenceError("");
    void fetch(`/purchases/${encodeURIComponent(expanded)}/evidence`, { credentials: "include" })
      .then(async (response) => {
        if (!response.ok) throw new Error(`証拠を取得できませんでした (${response.status})`);
        return (await response.json()) as { evidence: PurchaseEvidence[] };
      })
      .then((payload) => active && setPurchaseEvidence(payload.evidence))
      .catch((reason) => active && setEvidenceError((reason as Error).message));
    return () => {
      active = false;
    };
  }, [expanded]);
  useEffect(() => {
    let active = true;
    void readGraphql<{ tags: Tag[] }>(tagsQuery, { householdId: household.id })
      .then((result) => active && setTags(result.tags))
      .catch((reason) => active && setBulkError((reason as Error).message));
    return () => {
      active = false;
    };
  }, [household.id]);
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
  useEffect(() => {
    setSelectedItems(new Set());
    setSelectedPurchases(new Set());
    selectionAnchors.current = { item: null, purchase: null };
    setBulkUndo(null);
    setPurchaseBulkUndo(null);
    setBulkError("");
  }, [household.id]);
  useEffect(() => {
    setSelectedItems(new Set());
    setSelectedPurchases(new Set());
    selectionAnchors.current = { item: null, purchase: null };
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
        householdId: household.id,
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
    const occurredAt = new Date(purchase.occurredAt);
    occurredAt.setMinutes(occurredAt.getMinutes() - occurredAt.getTimezoneOffset());
    setEditing(purchase.id);
    setPurchaseDraft({
      merchant: purchase.merchant,
      occurredAt: occurredAt.toISOString().slice(0, 16),
      paymentAllocations: purchase.paymentAllocations.map((payment) => ({
        paymentAccountId: payment.paymentAccountId,
        amountYen: String(payment.amountYen),
      })),
    });
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
      await mutateCorrectPurchase({
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
    const inputForRevision = (baseRevision: number) => ({
      itemId: item.id,
      baseRevision,
      ...changedPatch,
      ...(quantityChanged
        ? {
            quantity: quantity
              ? {
                  numerator: quantity.numerator,
                  denominator: quantity.denominator,
                  unit: quantity.unit,
                }
              : null,
          }
        : {}),
      ...(printedPriceChanged
        ? {
            printedUnitPrice: hasPrintedPrice
              ? {
                  amountYen: printedPrice as number,
                  basis: {
                    numerator: printedBasis?.numerator as string,
                    denominator: printedBasis?.denominator as string,
                    unit: printedBasis?.unit as string,
                  },
                }
              : null,
          }
        : {}),
      ...(baseChangedFields.includes("productVariantId") && patch.productVariantId
        ? {
            productLearning: draft.productLearning,
            ...(draft.productLearning === "VARIANT_NAME"
              ? { variantName: draft.variantName.trim() }
              : {}),
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
  const filtered = purchases.filter((purchase) => {
    const month = purchaseMonth(purchase.occurredAt);
    return (
      (!fromMonth || month >= fromMonth) &&
      (!toMonth || month <= toMonth) &&
      (!search ||
        `${purchase.merchant} ${purchase.items.map((item) => item.name).join(" ")}`
          .toLocaleLowerCase()
          .includes(search.toLocaleLowerCase())) &&
      (!categoryFilter ||
        purchase.items.some((item) =>
          categoryFilter === "__uncategorized"
            ? !item.categoryId
            : item.categoryId === categoryFilter,
        )) &&
      (!poolFilter ||
        purchase.items.some((item) =>
          item.allocations.some((allocation) => allocation.poolId === poolFilter),
        )) &&
      (!accountFilter ||
        purchase.paymentAllocations.some(
          (payment) => payment.paymentAccountId === accountFilter,
        )) &&
      (!tagFilter ||
        purchase.items.some((item) => item.tags.some((tag) => tag.id === tagFilter))) &&
      (!minimumAmount || purchase.totalYen >= Number(minimumAmount)) &&
      (!maximumAmount || purchase.totalYen <= Number(maximumAmount))
    );
  });
  const filteredItems = purchases.flatMap((purchase) => {
    const month = purchaseMonth(purchase.occurredAt);
    const purchaseMatches =
      (!fromMonth || month >= fromMonth) &&
      (!toMonth || month <= toMonth) &&
      (!accountFilter ||
        purchase.paymentAllocations.some((payment) => payment.paymentAccountId === accountFilter));
    if (!purchaseMatches) return [];
    return purchase.items
      .filter((item) => {
        const searchMatches =
          !search ||
          `${purchase.merchant} ${item.name}`
            .toLocaleLowerCase()
            .includes(search.toLocaleLowerCase());
        return (
          searchMatches &&
          (!categoryFilter ||
            (categoryFilter === "__uncategorized"
              ? !item.categoryId
              : item.categoryId === categoryFilter)) &&
          (!poolFilter ||
            item.allocations.some((allocation) => allocation.poolId === poolFilter)) &&
          (!tagFilter || item.tags.some((tag) => tag.id === tagFilter)) &&
          (!minimumAmount || item.amountYen >= Number(minimumAmount)) &&
          (!maximumAmount || item.amountYen <= Number(maximumAmount))
        );
      })
      .map((item) => ({ purchase, item }));
  });
  const itemCoverageComplete = !loadingMore && !hasNextPage && !moreError;
  const selectedCount = selectedItems.size;
  const selectedPurchaseCount = selectedPurchases.size;
  const bulkAllocationEntries = Object.entries(bulkAllocations)
    .filter(([, weight]) => Number(weight) > 0)
    .map(([poolId, weight]) => ({ poolId, weight: Number(weight) }));
  const toggleSelection = (
    kind: "item" | "purchase",
    context: string,
    visibleIds: string[],
    id: string,
    shiftKey: boolean,
  ) => {
    const current = kind === "item" ? selectedItems : selectedPurchases;
    const anchor = selectionAnchors.current[kind];
    const anchorIndex =
      shiftKey && anchor?.context === context ? visibleIds.indexOf(anchor.id) : -1;
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
      setBulkError(kind === "item" ? "一括変更は100品目までです" : "一括変更は100件までです");
      return;
    }
    const next = new Set(current);
    if (range) range.forEach((targetId) => next.add(targetId));
    else if (next.has(id)) next.delete(id);
    else next.add(id);
    if (!shiftKey) selectionAnchors.current[kind] = { context, id };
    setBulkError("");
    if (kind === "item") setSelectedItems(next);
    else setSelectedPurchases(next);
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
      selectionAnchors.current.item = null;
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
  const applyBulkPurchaseEdit = async () => {
    const targets = purchases.filter((purchase) => selectedPurchases.has(purchase.id));
    const changedFields = [
      ...(bulkMerchantActive ? ["merchant"] : []),
      ...(bulkOccurredAtActive ? ["occurredAt"] : []),
      ...(bulkPaymentAccountId ? ["paymentAllocations"] : []),
    ];
    if (!targets.length || !changedFields.length) return;
    const occurredAt = bulkOccurredAtActive ? new Date(bulkOccurredAt).toISOString() : undefined;
    setBulkPending(true);
    setBulkError("");
    setPurchaseBulkUndo(null);
    const undoEntries: NonNullable<typeof purchaseBulkUndo> = [];
    try {
      const result = await mutateBulkUpdatePurchases({
        input: {
          targets: targets.map((purchase) => ({
            purchaseId: purchase.id,
            baseRevision: purchase.revision,
            patch: {
              ...(bulkMerchantActive ? { merchant: bulkMerchant.trim() } : {}),
              ...(occurredAt ? { occurredAt } : {}),
              ...(bulkPaymentAccountId
                ? {
                    paymentAllocations: [
                      { paymentAccountId: bulkPaymentAccountId, amountYen: purchase.totalYen },
                    ],
                  }
                : {}),
            },
          })),
        },
      });
      for (const purchase of targets) {
        const patch: Record<string, unknown> = {};
        if (bulkMerchantActive) patch.merchant = purchase.merchant;
        if (bulkOccurredAtActive) patch.occurredAt = purchase.occurredAt;
        if (bulkPaymentAccountId)
          patch.paymentAllocations = purchase.paymentAllocations.map(
            ({ paymentAccountId, amountYen }) => ({
              paymentAccountId,
              amountYen,
            }),
          );
        undoEntries.push({
          purchaseId: purchase.id,
          baseRevision:
            result.bulkUpdatePurchases.purchases.find((updated) => updated.id === purchase.id)
              ?.revision ?? purchase.revision + 1,
          patch,
        });
      }
      setPurchaseBulkUndo(undoEntries.length ? undoEntries : null);
      setSelectedPurchases(new Set());
      selectionAnchors.current.purchase = null;
      setBulkMerchantActive(false);
      setBulkMerchant("");
      setBulkOccurredAtActive(false);
      setBulkOccurredAt("");
      setBulkPaymentAccountId("");
      await refresh();
    } catch (reason) {
      setBulkError((reason as Error).message);
      setPurchaseBulkUndo(undoEntries.length ? undoEntries : null);
      setSelectedPurchases(new Set());
      selectionAnchors.current.purchase = null;
      await refresh();
    } finally {
      setBulkPending(false);
    }
  };
  const undoBulkPurchaseEdit = async () => {
    if (!purchaseBulkUndo?.length) return;
    setBulkPending(true);
    setBulkError("");
    try {
      await mutateBulkUpdatePurchases({
        input: {
          targets: purchaseBulkUndo.map((entry) => ({
            purchaseId: entry.purchaseId,
            baseRevision: entry.baseRevision,
            patch: entry.patch,
          })),
        },
      });
      setPurchaseBulkUndo(null);
      await refresh();
    } catch (reason) {
      setBulkError((reason as Error).message);
      await refresh();
    } finally {
      setBulkPending(false);
    }
  };
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
        {recordMode === "purchases" && (
          <label className="input-with-icon">
            <Search size={16} />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
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
            onChange={(event) => setFromMonth(event.target.value)}
          />
        </label>
        <label className="select-filter">
          <span>終了</span>
          <input
            aria-label="終了月"
            type="month"
            value={toMonth}
            onChange={(event) => setToMonth(event.target.value)}
          />
        </label>
        <div className="record-count">
          {recordMode === "observations"
            ? `${unmatched.observations.length}/${unmatched.totalCount}件表示`
            : viewMode === "items"
              ? `${filteredItems.length}品目表示 · ${purchases.length}/${totalCount}件購入取得`
              : `${filtered.length}件表示 · ${purchases.length}/${totalCount}件読込`}
        </div>
      </div>
      <div className="ledger-filters">
        <label className="select-filter">
          <span>表示</span>
          <select
            value={recordMode}
            onChange={(event) => {
              const mode = event.target.value as "purchases" | "observations";
              setRecordMode(mode);
              if (mode === "observations") {
                setSelectedItems(new Set());
                selectionAnchors.current.item = null;
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
                setViewMode(event.target.value as "purchases" | "items");
                setSelectedItems(new Set());
                selectionAnchors.current.item = null;
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
                onChange={(event) => setOriginFilter(event.target.value as Purchase["origin"] | "")}
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
                onChange={(event) => setEvidenceFilter(event.target.value)}
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
                onChange={(event) => setCategoryFilter(event.target.value)}
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
              <select value={poolFilter} onChange={(event) => setPoolFilter(event.target.value)}>
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
                onChange={(event) => setAccountFilter(event.target.value)}
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
              <select value={tagFilter} onChange={(event) => setTagFilter(event.target.value)}>
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
                onChange={(event) => setMinimumAmount(event.target.value)}
              />
              <span>〜</span>
              <input
                aria-label="最高金額"
                type="number"
                min="0"
                placeholder="上限"
                value={maximumAmount}
                onChange={(event) => setMaximumAmount(event.target.value)}
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
                  setObservationStateFilter(event.target.value as UnmatchedObservationState | "")
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
                  setObservationSourceFilter(event.target.value as SourceKind | "")
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
      {recordMode === "purchases" && viewMode === "purchases" && selectedPurchaseCount > 0 && (
        <section className="panel ledger-bulk-actions" aria-label="選択した購入の一括編集">
          <span>{selectedPurchaseCount}件の購入を選択中</span>
          <label>
            <input
              type="checkbox"
              checked={bulkMerchantActive}
              disabled={bulkPending}
              onChange={(event) => setBulkMerchantActive(event.target.checked)}
            />
            店舗
            <input
              type="text"
              value={bulkMerchant}
              disabled={!bulkMerchantActive || bulkPending}
              onChange={(event) => setBulkMerchant(event.target.value)}
            />
          </label>
          <label>
            <input
              type="checkbox"
              checked={bulkOccurredAtActive}
              disabled={bulkPending}
              onChange={(event) => setBulkOccurredAtActive(event.target.checked)}
            />
            日付
            <input
              type="datetime-local"
              value={bulkOccurredAt}
              disabled={!bulkOccurredAtActive || bulkPending}
              onChange={(event) => setBulkOccurredAt(event.target.value)}
            />
          </label>
          <label className="select-filter">
            <span>決済元</span>
            <select
              value={bulkPaymentAccountId}
              disabled={bulkPending}
              onChange={(event) => setBulkPaymentAccountId(event.target.value)}
            >
              <option value="">変更なし</option>
              {household.accounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
            </select>
          </label>
          <button
            className="button button-primary"
            type="button"
            disabled={
              bulkPending ||
              (!bulkMerchantActive && !bulkOccurredAtActive && !bulkPaymentAccountId) ||
              (bulkOccurredAtActive && !bulkOccurredAt)
            }
            onClick={() => void applyBulkPurchaseEdit()}
          >
            {bulkPending ? "更新中" : "一括変更"}
          </button>
          <button
            className="text-button"
            type="button"
            disabled={bulkPending}
            onClick={() => {
              setSelectedPurchases(new Set());
              selectionAnchors.current.purchase = null;
            }}
          >
            選択解除
          </button>
        </section>
      )}
      {purchaseBulkUndo && (
        <section className="panel ledger-bulk-actions" aria-live="polite">
          <span>{purchaseBulkUndo.length}件の購入変更</span>
          <button
            className="text-button"
            type="button"
            disabled={bulkPending}
            onClick={() => void undoBulkPurchaseEdit()}
          >
            一括変更を元に戻す
          </button>
        </section>
      )}
      {selectedCount > 0 && (
        <section className="panel ledger-bulk-actions" aria-label="選択品目の一括編集">
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
              selectionAnchors.current.item = null;
            }}
          >
            選択解除
          </button>
        </section>
      )}
      {bulkUndo && (
        <section className="panel ledger-bulk-actions" aria-live="polite">
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
        <section className="panel ledger-list">
          {unmatched.loading ? (
            <div className="skeleton-list">
              <i />
              <i />
              <i />
            </div>
          ) : unmatched.error ? (
            <InlineError message={unmatched.error} />
          ) : unmatched.observations.length === 0 ? (
            <div className="empty-state compact">
              <BookOpen />
              <span>照合候補がありません</span>
            </div>
          ) : (
            unmatched.observations.map((observation) => (
              <div
                className="ledger-entry"
                key={`${observation.documentId}-${observation.sourceReference}`}
              >
                <div className="merchant-icon">{observation.sourceKind.slice(0, 1)}</div>
                <div className="purchase-description">
                  <b>{observation.merchant || "店舗未入力"}</b>
                  <span>
                    {observation.sourceKind} · {observation.sourceReference}
                    {observation.orderId ? ` · 注文 ${observation.orderId}` : ""}
                    {observation.transactionId ? ` · 取引 ${observation.transactionId}` : ""}
                  </span>
                </div>
                <div className="purchase-meta">
                  <b>{yen(observation.amountYen)}</b>
                  <span>{observation.state === "CONFIRMED" ? "照合済み" : "未照合"}</span>
                </div>
                <a
                  href={`/documents/${encodeURIComponent(observation.documentId)}/content`}
                  target="_blank"
                  rel="noreferrer"
                >
                  原本を開く
                </a>
                {observation.state === "UNMATCHED" && (
                  <ObservationLinker
                    household={household}
                    observation={observation}
                    onConfirmed={() => void unmatched.refresh()}
                  />
                )}
              </div>
            ))
          )}
          {unmatched.moreError && <InlineError message={unmatched.moreError} />}
          {unmatched.hasNextPage && (
            <div className="ledger-more">
              <button
                className="button button-soft"
                disabled={unmatched.loadingMore}
                onClick={() => void unmatched.loadMore()}
              >
                {unmatched.loadingMore ? "読み込み中" : "さらに読み込む"}
              </button>
            </div>
          )}
        </section>
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
                        "item",
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
                      setRecordMode("purchases");
                      setViewMode("purchases");
                      setExpanded(purchase.id);
                    }}
                  >
                    購入を開く
                  </button>
                </div>
              </div>
            ))
          )}
          {loadingMore && <div className="ledger-more">購入履歴を取得中</div>}
          {moreError && <InlineError message={moreError} />}
          {moreError && hasNextPage && (
            <div className="ledger-more">
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
                    checked={selectedPurchases.has(purchase.id)}
                    disabled={bulkPending || purchase.status === "CLOSED"}
                    onChange={(event) =>
                      toggleSelection(
                        "purchase",
                        "purchases",
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
                    {(purchaseEvidence.length > 0 || evidenceError) && (
                      <section className="panel ledger-evidence-panel">
                        <div className="panel-heading">
                          <div>
                            <span className="eyebrow">購入の証拠</span>
                            <h3>{purchaseEvidence.length}件</h3>
                          </div>
                        </div>
                        {evidenceError && <InlineError message={evidenceError} />}
                        {purchaseEvidence.map((entry, index) => {
                          const linkedItem = purchase.items.find(
                            (item) => item.id === entry.itemId,
                          );
                          return (
                            <div
                              className="ledger-evidence-entry"
                              key={`${entry.documentId}-${entry.itemId ?? "purchase"}-${index}`}
                            >
                              <a
                                href={`/documents/${encodeURIComponent(entry.documentId)}/content`}
                                target="_blank"
                                rel="noreferrer"
                              >
                                {entry.sourceKind} · {linkedItem?.name ?? "購入全体"}を開く
                              </a>
                              {entry.mimeType.startsWith("image/") && (
                                <div className="evidence-image-stage">
                                  <img
                                    src={`/documents/${encodeURIComponent(entry.documentId)}/content`}
                                    alt={`${entry.sourceKind}の原本`}
                                  />
                                  {entry.region && (
                                    <button
                                      type="button"
                                      className={`evidence-region ${highlightedEvidenceItem === entry.itemId ? "evidence-region-selected" : ""}`}
                                      style={{
                                        top: `${entry.region.top * 100}%`,
                                        left: `${entry.region.left * 100}%`,
                                        width: `${(entry.region.right - entry.region.left) * 100}%`,
                                        height: `${(entry.region.bottom - entry.region.top) * 100}%`,
                                      }}
                                      aria-label={`${linkedItem?.name ?? "購入"}の原本位置`}
                                      onClick={() =>
                                        setHighlightedEvidenceItem(entry.itemId ?? null)
                                      }
                                    />
                                  )}
                                </div>
                              )}
                              {entry.mimeType === "application/pdf" && (
                                <iframe
                                  className="evidence-pdf-preview"
                                  title="証拠原本"
                                  src={`/documents/${encodeURIComponent(entry.documentId)}/content`}
                                />
                              )}
                              {linkedItem && <span>品目 {linkedItem.name}</span>}
                            </div>
                          );
                        })}
                      </section>
                    )}
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
                                          ? {
                                              ...row,
                                              amountYen: event.target.value.replace(/\D/g, ""),
                                            }
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
                            差額{" "}
                            {yen(
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
                              "item",
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
      )}
      {recordMode === "purchases" && moreError && <InlineError message={moreError} />}
      {recordMode === "purchases" && hasNextPage && !filtersActive && (
        <div className="ledger-more">
          <button
            className="button button-soft"
            disabled={loadingMore}
            onClick={() => void loadMore()}
          >
            {loadingMore ? "読み込み中" : "古い記録を読み込む"}
          </button>
        </div>
      )}
      {recordMode === "purchases" && moreError && filtersActive && (
        <div className="ledger-more">
          <button
            className="button button-soft"
            disabled={loadingMore}
            onClick={() => void loadMore()}
          >
            {loadingMore ? "読み込み中" : "検索範囲を読み込む"}
          </button>
        </div>
      )}
    </>
  );
}

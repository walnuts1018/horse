import { useCallback, useEffect, useRef, useState, type SubmitEvent } from "react";

import type { Household, ProductConcept } from "../../../shared/api";
import type { CatalogAllocationRule } from "../api/allocation-catalog";

import { errorMessage } from "../../../shared/lib";
import {
  createCatalogAlias,
  createCatalogConcept,
  createCatalogVariant,
  readAllocationRules,
  readCatalog,
  setAllocationRule,
} from "../api/allocation-catalog";

type AllocationRuleDraft = {
  scope: "CATEGORY" | "MERCHANT" | "PRODUCT" | "PRODUCT_VARIANT";
  categoryId: string;
  merchant: string;
  productId: string;
  variantId: string;
  weights: Record<string, string>;
};

export function createAllocationRuleDraft(
  rule: CatalogAllocationRule,
  pools: Household["pools"],
): AllocationRuleDraft | null {
  const target = rule.target;
  if (target.__typename !== "MerchantAllocationRuleTarget" && !target.id) return null;
  return {
    scope:
      target.__typename === "Category"
        ? "CATEGORY"
        : target.__typename === "MerchantAllocationRuleTarget"
          ? "MERCHANT"
          : target.__typename === "ProductConcept"
            ? "PRODUCT"
            : "PRODUCT_VARIANT",
    categoryId: target.__typename === "Category" ? (target.id ?? "") : "",
    merchant: target.__typename === "MerchantAllocationRuleTarget" ? target.name : "",
    productId: target.__typename === "ProductConcept" ? (target.id ?? "") : "",
    variantId: target.__typename === "ProductVariant" ? (target.id ?? "") : "",
    weights: Object.fromEntries(
      pools.map((pool) => [
        pool.id,
        String(rule.allocations.find((allocation) => allocation.poolId === pool.id)?.weight ?? 0),
      ]),
    ),
  };
}

export function useAllocationCatalog({
  household,
  onNotice,
  onFormClean,
}: {
  household: Household;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormClean: (formID: string) => void;
}) {
  const onNoticeRef = useRef(onNotice);
  const onFormCleanRef = useRef(onFormClean);
  const [allocationRules, setAllocationRules] = useState<CatalogAllocationRule[]>([]);
  const [allocationRulesCount, setAllocationRulesCount] = useState(0);
  const [allocationRulesCursor, setAllocationRulesCursor] = useState<string | null>(null);
  const [allocationRulesHasNextPage, setAllocationRulesHasNextPage] = useState(false);
  const [allocationRulesPageLoading, setAllocationRulesPageLoading] = useState(false);
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

  // pools変更時はruleWeightsを即時導出し、effectでの同期setStateを廃止
  const ruleWeightsDerived =
    household.pools.length === 0
      ? {}
      : Object.fromEntries(
          household.pools.map((pool) => [pool.id, String(pool.defaultShareWeight || 1)]),
        );
  const effectiveRuleWeights =
    Object.keys(ruleWeights).length === 0 ? ruleWeightsDerived : ruleWeights;
  const [catalog, setCatalog] = useState<ProductConcept[]>([]);
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
  const [editingRuleId, setEditingRuleId] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    onNoticeRef.current = onNotice;
    onFormCleanRef.current = onFormClean;
  }, [onFormClean, onNotice]);

  const refreshRules = useCallback(async () => {
    const result = await readAllocationRules(household.id);
    setAllocationRules(result.edges.map((edge) => edge.node));
    setAllocationRulesCount(result.totalCount);
    setAllocationRulesCursor(result.pageInfo.endCursor ?? null);
    setAllocationRulesHasNextPage(result.pageInfo.hasNextPage);
  }, [household.id]);

  const refreshCatalog = useCallback(async () => {
    setCatalog(await readCatalog(household.id));
  }, [household.id]);

  useEffect(() => {
    // 初回マウント時の取得をマイクロタスクで遅延し、同期setStateを排除
    queueMicrotask(() => {
      void Promise.all([refreshRules(), refreshCatalog()]).catch((reason) =>
        onNoticeRef.current(errorMessage(reason), "error"),
      );
    });
  }, [refreshRules, refreshCatalog]);

  const loadMoreRules = async () => {
    if (!allocationRulesCursor || allocationRulesPageLoading) return;
    setAllocationRulesPageLoading(true);
    try {
      const result = await readAllocationRules(household.id, allocationRulesCursor);
      setAllocationRules((current) => [...current, ...result.edges.map((edge) => edge.node)]);
      setAllocationRulesCursor(result.pageInfo.endCursor ?? null);
      setAllocationRulesHasNextPage(result.pageInfo.hasNextPage);
    } catch (reason) {
      onNoticeRef.current(errorMessage(reason), "error");
    } finally {
      setAllocationRulesPageLoading(false);
    }
  };

  const editRule = (rule: CatalogAllocationRule) => {
    const draft = createAllocationRuleDraft(rule, household.pools);
    if (!draft) return;
    setRuleScope(draft.scope);
    setRuleCategoryId(draft.categoryId);
    setRuleMerchant(draft.merchant);
    setRuleProductId(draft.productId);
    setRuleVariantId(draft.variantId);
    setRuleWeights(draft.weights);
    setEditingRuleId(rule.id);
  };

  const cancelRuleEdit = () => {
    setEditingRuleId(null);
    setRuleScope("CATEGORY");
    setRuleCategoryId("");
    setRuleMerchant("");
    setRuleProductId("");
    setRuleVariantId("");
    setRuleWeights({});
  };

  const saveRule = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    const allocations = household.pools
      .map((pool) => ({
        poolId: pool.id,
        weight: Math.max(0, Number(effectiveRuleWeights[pool.id]) || 0),
      }))
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
      const target =
        ruleScope === "CATEGORY"
          ? { categoryId: ruleCategoryId }
          : ruleScope === "MERCHANT"
            ? { merchant: ruleMerchant.trim() }
            : ruleScope === "PRODUCT"
              ? { productConceptId: ruleProductId }
              : { productVariantId: ruleVariantId };
      await setAllocationRule({ householdId: household.id, target, allocations });
      onFormCleanRef.current("allocation-rule");
      await refreshRules();
      onNoticeRef.current(
        editingRuleId ? "負担ルールを更新しました" : "負担ルールを保存しました",
        "success",
      );
      cancelRuleEdit();
    } catch (reason) {
      onNoticeRef.current(errorMessage(reason), "error");
    } finally {
      setPending(false);
    }
  };

  const createConcept = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPending(true);
    try {
      await createCatalogConcept({
        householdId: household.id,
        name: conceptName.trim(),
        categoryId: conceptCategoryId || null,
      });
      onFormCleanRef.current("create-concept");
      setConceptName("");
      await refreshCatalog();
      onNoticeRef.current("商品を追加しました", "success");
    } catch (reason) {
      onNoticeRef.current(errorMessage(reason), "error");
    } finally {
      setPending(false);
    }
  };

  const createVariant = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!variantConceptId) return;
    setPending(true);
    try {
      await createCatalogVariant({
        householdId: household.id,
        conceptId: variantConceptId,
        name: variantName.trim(),
        brand: variantBrand.trim() || null,
        packageSize: variantPackageSize.trim() || null,
      });
      onFormCleanRef.current("create-variant");
      setVariantName("");
      setVariantBrand("");
      setVariantPackageSize("");
      await refreshCatalog();
      onNoticeRef.current("バリエーションを追加しました", "success");
    } catch (reason) {
      onNoticeRef.current(errorMessage(reason), "error");
    } finally {
      setPending(false);
    }
  };

  const createAlias = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!aliasVariantId) return;
    setPending(true);
    try {
      await createCatalogAlias({
        householdId: household.id,
        variantId: aliasVariantId,
        merchant: aliasMerchant.trim(),
        alias: aliasName.trim(),
      });
      onFormCleanRef.current("create-alias");
      setAliasName("");
      await refreshCatalog();
      onNoticeRef.current("店名の別表記を登録しました", "success");
    } catch (reason) {
      onNoticeRef.current(errorMessage(reason), "error");
    } finally {
      setPending(false);
    }
  };

  return {
    allocationRules,
    allocationRulesCount,
    allocationRulesHasNextPage,
    allocationRulesPageLoading,
    catalog,
    conceptName,
    setConceptName,
    conceptCategoryId,
    setConceptCategoryId,
    variantConceptId,
    setVariantConceptId,
    variantName,
    setVariantName,
    variantBrand,
    setVariantBrand,
    variantPackageSize,
    setVariantPackageSize,
    aliasVariantId,
    setAliasVariantId,
    aliasMerchant,
    setAliasMerchant,
    aliasName,
    setAliasName,
    ruleScope,
    setRuleScope,
    ruleCategoryId,
    setRuleCategoryId,
    ruleMerchant,
    setRuleMerchant,
    ruleWeights: effectiveRuleWeights,
    setRuleWeights,
    ruleProductId,
    setRuleProductId,
    ruleVariantId,
    setRuleVariantId,
    editingRuleId,
    pending,
    loadMoreRules,
    editRule,
    cancelRuleEdit,
    saveRule,
    createConcept,
    createVariant,
    createAlias,
  };
}

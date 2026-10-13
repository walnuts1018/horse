import { useEffect, useRef, useState, type FormEvent } from "react";
import type { AllocationRule, Household, ProductConcept } from "../../../shared/api";
import {
  createCatalogAlias,
  createCatalogConcept,
  createCatalogVariant,
  readAllocationRules,
  readCatalog,
  setAllocationRule,
} from "../api/allocation-catalog";

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
  const [allocationRules, setAllocationRules] = useState<AllocationRule[]>([]);
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
  const [pending, setPending] = useState(false);

  useEffect(() => {
    onNoticeRef.current = onNotice;
    onFormCleanRef.current = onFormClean;
  }, [onFormClean, onNotice]);

  const refreshRules = async () => {
    const result = await readAllocationRules(household.id);
    setAllocationRules(result.edges.map((edge) => edge.node));
    setAllocationRulesCount(result.totalCount);
    setAllocationRulesCursor(result.pageInfo.endCursor ?? null);
    setAllocationRulesHasNextPage(result.pageInfo.hasNextPage);
  };

  const refreshCatalog = async () => {
    setCatalog(await readCatalog(household.id));
  };

  useEffect(() => {
    void Promise.all([refreshRules(), refreshCatalog()]).catch((reason) =>
      onNoticeRef.current((reason as Error).message, "error"),
    );
  }, [household.id]);

  useEffect(() => {
    setRuleWeights(
      Object.fromEntries(
        household.pools.map((pool) => [pool.id, String(pool.defaultShareWeight || 1)]),
      ),
    );
  }, [household.id, household.pools]);

  const loadMoreRules = async () => {
    if (!allocationRulesCursor || allocationRulesPageLoading) return;
    setAllocationRulesPageLoading(true);
    try {
      const result = await readAllocationRules(household.id, allocationRulesCursor);
      setAllocationRules((current) => [...current, ...result.edges.map((edge) => edge.node)]);
      setAllocationRulesCursor(result.pageInfo.endCursor ?? null);
      setAllocationRulesHasNextPage(result.pageInfo.hasNextPage);
    } catch (reason) {
      onNoticeRef.current((reason as Error).message, "error");
    } finally {
      setAllocationRulesPageLoading(false);
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
      onNoticeRef.current("負担ルールを保存しました", "success");
      setRuleMerchant("");
    } catch (reason) {
      onNoticeRef.current((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };

  const createConcept = async (event: FormEvent) => {
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
      onNoticeRef.current((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };

  const createVariant = async (event: FormEvent) => {
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
      onNoticeRef.current((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };

  const createAlias = async (event: FormEvent) => {
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
      onNoticeRef.current((reason as Error).message, "error");
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
    ruleWeights,
    setRuleWeights,
    ruleProductId,
    setRuleProductId,
    ruleVariantId,
    setRuleVariantId,
    pending,
    loadMoreRules,
    saveRule,
    createConcept,
    createVariant,
    createAlias,
  };
}

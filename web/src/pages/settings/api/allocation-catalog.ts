import { fetchQuery } from "react-relay";

import { readProductCatalog } from "../../../entities/product";
import {
  createProductConceptMutation,
  createProductVariantMutation,
  setProductAliasMutation,
} from "../../../entities/product";
import {
  getRelayEnvironment,
  type AllocationRule,
  type AllocationRulesQuery,
  type AllocationRulesQuery$data,
  type CreateProductConceptMutation,
  type CreateProductVariantMutation,
  type SetAllocationRuleMutation,
  type SetProductAliasMutation,
} from "../../../shared/api";
import { commitSettingsMutation } from "./commit-mutation";
import { allocationRulesQuery, setAllocationRuleMutation } from "./operations";

export async function readAllocationRules(householdId: string, after?: string) {
  const data = await fetchQuery<AllocationRulesQuery>(
    getRelayEnvironment(),
    allocationRulesQuery,
    { householdId, first: 50, ...(after ? { after } : {}) },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("負担ルールを取得できませんでした");
  return mapAllocationRules(data.allocationRules);
}

function mapAllocationRules(page: AllocationRulesQuery$data["allocationRules"]) {
  return {
    edges: page.edges.map(({ cursor, node }) => ({ cursor, node: mapAllocationRule(node) })),
    pageInfo: {
      endCursor: page.pageInfo.endCursor ?? null,
      hasNextPage: page.pageInfo.hasNextPage,
    },
    totalCount: page.totalCount,
  };
}

function mapAllocationRule(
  rule: AllocationRulesQuery$data["allocationRules"]["edges"][number]["node"],
): AllocationRule {
  return {
    id: rule.id,
    householdId: rule.householdId,
    target: mapAllocationRuleTarget(rule.target.__typename, rule.target.name),
    revision: rule.revision,
    allocations: rule.allocations.map((allocation) => ({
      poolId: allocation.poolId,
      poolName: allocation.poolName,
      weight: allocation.weight,
    })),
  };
}

function mapAllocationRuleTarget(typename: string, name: string): AllocationRule["target"] {
  switch (typename) {
    case "Category":
    case "ProductConcept":
    case "ProductVariant":
    case "MerchantAllocationRuleTarget":
      return { __typename: typename, name };
    default:
      throw new Error("負担ルールの対象種別を判別できません");
  }
}

export function readCatalog(householdId: string) {
  return readProductCatalog(householdId);
}

export function setAllocationRule(input: SetAllocationRuleMutation["variables"]["input"]) {
  return commitSettingsMutation<
    SetAllocationRuleMutation,
    NonNullable<SetAllocationRuleMutation["response"]["setAllocationRule"]["result"]>
  >(
    setAllocationRuleMutation,
    { input },
    (response) => response.setAllocationRule,
    "SetAllocationRuleSuccess",
    "負担ルールを保存できませんでした",
  );
}

export function createCatalogConcept(input: CreateProductConceptMutation["variables"]["input"]) {
  return commitSettingsMutation<
    CreateProductConceptMutation,
    NonNullable<CreateProductConceptMutation["response"]["createProductConcept"]["result"]>
  >(
    createProductConceptMutation,
    { input },
    (response) => response.createProductConcept,
    "CreateProductConceptSuccess",
    "商品分類を作成できませんでした",
  );
}

export function createCatalogVariant(input: CreateProductVariantMutation["variables"]["input"]) {
  return commitSettingsMutation<
    CreateProductVariantMutation,
    NonNullable<CreateProductVariantMutation["response"]["createProductVariant"]["result"]>
  >(
    createProductVariantMutation,
    { input },
    (response) => response.createProductVariant,
    "CreateProductVariantSuccess",
    "商品バリエーションを作成できませんでした",
  );
}

export function createCatalogAlias(input: SetProductAliasMutation["variables"]["input"]) {
  return commitSettingsMutation<
    SetProductAliasMutation,
    NonNullable<SetProductAliasMutation["response"]["setProductAlias"]["result"]>
  >(
    setProductAliasMutation,
    { input },
    (response) => response.setProductAlias,
    "SetProductAliasSuccess",
    "商品別名を保存できませんでした",
  );
}

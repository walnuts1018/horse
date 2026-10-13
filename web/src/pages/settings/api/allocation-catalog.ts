import { fetchQuery } from "react-relay";
import {
  getRelayEnvironment,
  readGraphql,
  type AllocationRule,
  type AllocationRulesQuery,
  type AllocationRulesQuery$data,
} from "../../../shared/api";
import { readProductCatalog } from "../../../entities/product";
import { allocationRulesQuery, setAllocationRuleMutation } from "./operations";
import {
  createProductConceptMutation,
  createProductVariantMutation,
  setProductAliasMutation,
} from "../../../entities/product";

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

export function setAllocationRule(input: Record<string, unknown>) {
  return readGraphql(setAllocationRuleMutation, { input });
}

export function createCatalogConcept(input: Record<string, unknown>) {
  return readGraphql(createProductConceptMutation, { input });
}

export function createCatalogVariant(input: Record<string, unknown>) {
  return readGraphql(createProductVariantMutation, { input });
}

export function createCatalogAlias(input: Record<string, unknown>) {
  return readGraphql(setProductAliasMutation, { input });
}

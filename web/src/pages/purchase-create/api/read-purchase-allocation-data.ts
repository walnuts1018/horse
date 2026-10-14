import { fetchQuery } from "react-relay";

import type { PurchaseAllocationRule } from "../model/purchase-allocation-rules";

import { readProductCatalog } from "../../../entities/product";
import {
  getRelayEnvironment,
  type ProductConcept,
  type PurchaseAllocationRulesQuery,
  type PurchaseAllocationRulesQuery$data,
} from "../../../shared/api";
import { purchaseAllocationRulesQuery } from "./purchase-allocation-rules-operations";

export type PurchaseAllocationData = {
  rules: PurchaseAllocationRule[];
  conceptIdsByVariant: ReadonlyMap<string, string>;
};

const firstPageSize = 100;

export async function readPurchaseAllocationData(
  householdId: string,
): Promise<PurchaseAllocationData> {
  const [rules, catalog] = await Promise.all([
    readPurchaseAllocationRules(householdId),
    readProductCatalog(householdId),
  ]);
  return { rules, conceptIdsByVariant: productConceptIds(catalog) };
}

async function readPurchaseAllocationRules(householdId: string) {
  const rules: PurchaseAllocationRule[] = [];
  let after: string | null = null;
  for (;;) {
    const data: PurchaseAllocationRulesQuery$data | undefined =
      await fetchQuery<PurchaseAllocationRulesQuery>(
        getRelayEnvironment(),
        purchaseAllocationRulesQuery,
        { householdId, first: firstPageSize, after },
        { fetchPolicy: "store-or-network" },
      ).toPromise();
    if (!data) throw new Error("負担ルールを取得できませんでした");
    rules.push(...data.allocationRules.edges.map(({ node }) => mapRule(node)));
    const pageInfo: PurchaseAllocationRulesQuery$data["allocationRules"]["pageInfo"] =
      data.allocationRules.pageInfo;
    const endCursor: string | null = pageInfo.endCursor ?? null;
    if (!pageInfo.hasNextPage || !endCursor || endCursor === after) return rules;
    after = endCursor;
  }
}

function mapRule(
  rule: PurchaseAllocationRulesQuery$data["allocationRules"]["edges"][number]["node"],
): PurchaseAllocationRule {
  const target = rule.target;
  switch (target.__typename) {
    case "Category":
      return {
        target: { kind: "category", id: target.id ?? "" },
        allocations: mapAllocations(rule.allocations),
      };
    case "ProductConcept":
      return {
        target: { kind: "concept", id: target.id ?? "" },
        allocations: mapAllocations(rule.allocations),
      };
    case "ProductVariant":
      return {
        target: { kind: "variant", id: target.id ?? "" },
        allocations: mapAllocations(rule.allocations),
      };
    case "MerchantAllocationRuleTarget":
      return {
        target: { kind: "merchant", key: normalizeMerchant(target.name) },
        allocations: mapAllocations(rule.allocations),
      };
    default:
      return { target: { kind: "merchant", key: "" }, allocations: [] };
  }
}

function mapAllocations(
  allocations: PurchaseAllocationRulesQuery$data["allocationRules"]["edges"][number]["node"]["allocations"],
) {
  return allocations.map(({ poolId, weight }) => ({ poolId, weight: Number(weight) }));
}

function productConceptIds(products: ProductConcept[]) {
  const conceptIds = new Map<string, string>();
  for (const concept of products) {
    for (const variant of concept.variants) conceptIds.set(variant.id, concept.id);
  }
  return conceptIds;
}

function normalizeMerchant(value: string) {
  return value.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean).join(" ");
}

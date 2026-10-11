/**
 * @generated SignedSource<<3978e8e10f8cd514292880c67a1c49c9>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AllocationRuleScope = "CATEGORY" | "MERCHANT" | "PRODUCT" | "PRODUCT_VARIANT" | "%future added value";
export type AllocationRulesQuery$variables = {
  householdId: string;
};
export type AllocationRulesQuery$data = {
  readonly allocationRules: ReadonlyArray<{
    readonly allocations: ReadonlyArray<{
      readonly amountYen: any | null | undefined;
      readonly poolId: string;
      readonly poolName: string | null | undefined;
      readonly weight: any;
    }>;
    readonly categoryId: string | null | undefined;
    readonly householdId: string;
    readonly id: string;
    readonly merchant: string | null | undefined;
    readonly productConceptId: string | null | undefined;
    readonly productVariantId: string | null | undefined;
    readonly revision: number;
    readonly scope: AllocationRuleScope;
  }>;
};
export type AllocationRulesQuery = {
  response: AllocationRulesQuery$data;
  variables: AllocationRulesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      }
    ],
    "concreteType": "AllocationRule",
    "kind": "LinkedField",
    "name": "allocationRules",
    "plural": true,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "id",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "householdId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "scope",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "categoryId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "merchant",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "productConceptId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "productVariantId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "revision",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "Allocation",
        "kind": "LinkedField",
        "name": "allocations",
        "plural": true,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "poolId",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "poolName",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "weight",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "amountYen",
            "storageKey": null
          }
        ],
        "storageKey": null
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "AllocationRulesQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AllocationRulesQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "1ce449ad44faacc0cc9a0ca4dcba0990",
    "id": null,
    "metadata": {},
    "name": "AllocationRulesQuery",
    "operationKind": "query",
    "text": "query AllocationRulesQuery(\n  $householdId: ID!\n) {\n  allocationRules(householdId: $householdId) {\n    id\n    householdId\n    scope\n    categoryId\n    merchant\n    productConceptId\n    productVariantId\n    revision\n    allocations {\n      poolId\n      poolName\n      weight\n      amountYen\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "8b958181f1aad9e4ef277bb005a474dd";

export default node;

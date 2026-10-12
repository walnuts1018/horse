/**
 * @generated SignedSource<<ea176ccf2ad6d6ceae92edcea0f82854>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountKind = "BANK" | "CARD" | "CASH" | "OTHER" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
export type PurchasesQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  from?: string | null | undefined;
  hasEvidence?: boolean | null | undefined;
  householdId: string;
  origin?: PurchaseOrigin | null | undefined;
  to?: string | null | undefined;
};
export type PurchasesQuery$data = {
  readonly purchases: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly corrections: ReadonlyArray<{
          readonly amountDeltaYen: any;
          readonly id: string;
          readonly itemId: string | null | undefined;
          readonly occurredAt: string;
          readonly paymentAllocations: ReadonlyArray<{
            readonly account: {
              readonly id: string;
              readonly name: string;
            } | null | undefined;
            readonly amountYen: any;
            readonly paymentAccountId: string;
          }>;
          readonly reason: string;
          readonly settlementMonth: string;
        }>;
        readonly createdAt: string;
        readonly hasEvidence: boolean;
        readonly householdId: string;
        readonly id: string;
        readonly items: ReadonlyArray<{
          readonly allocations: ReadonlyArray<{
            readonly amountYen: any | null | undefined;
            readonly poolId: string;
            readonly poolName: string | null | undefined;
            readonly weight: any;
          }>;
          readonly amountYen: any;
          readonly categoryId: string | null | undefined;
          readonly effectiveUnitPriceDenominator: string | null | undefined;
          readonly effectiveUnitPriceNumeratorYen: string | null | undefined;
          readonly id: string;
          readonly name: string;
          readonly printedUnitPriceQuantityDenominator: string | null | undefined;
          readonly printedUnitPriceQuantityNumerator: string | null | undefined;
          readonly printedUnitPriceUnit: string | null | undefined;
          readonly printedUnitPriceYen: any | null | undefined;
          readonly productVariantId: string | null | undefined;
          readonly quantityDenominator: string | null | undefined;
          readonly quantityNumerator: string | null | undefined;
          readonly quantityUnit: string | null | undefined;
          readonly revision: number;
          readonly tags: ReadonlyArray<{
            readonly archived: boolean;
            readonly householdId: string;
            readonly id: string;
            readonly name: string;
            readonly revision: number;
          }>;
        }>;
        readonly merchant: string;
        readonly occurredAt: string;
        readonly origin: PurchaseOrigin;
        readonly paymentAccount: {
          readonly id: string;
          readonly kind: AccountKind;
          readonly name: string;
          readonly poolId: string | null | undefined;
        } | null | undefined;
        readonly paymentAccountId: string | null | undefined;
        readonly paymentAllocations: ReadonlyArray<{
          readonly account: {
            readonly id: string;
            readonly name: string;
          } | null | undefined;
          readonly amountYen: any;
          readonly paymentAccountId: string;
        }>;
        readonly revision: number;
        readonly sourceKinds: ReadonlyArray<SourceKind>;
        readonly status: PurchaseStatus;
        readonly totalYen: any;
        readonly updatedAt: string;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
    readonly totalCount: number;
  };
};
export type PurchasesQuery = {
  response: PurchasesQuery$data;
  variables: PurchasesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "after"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "from"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "hasEvidence"
},
v4 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v5 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "origin"
},
v6 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v9 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
},
v10 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "paymentAccountId",
  "storageKey": null
},
v11 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v12 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "poolId",
  "storageKey": null
},
v13 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v14 = {
  "alias": null,
  "args": null,
  "concreteType": "PurchasePayment",
  "kind": "LinkedField",
  "name": "paymentAllocations",
  "plural": true,
  "selections": [
    (v10/*:: as any*/),
    (v13/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "concreteType": "Account",
      "kind": "LinkedField",
      "name": "account",
      "plural": false,
      "selections": [
        (v7/*:: as any*/),
        (v11/*:: as any*/)
      ],
      "storageKey": null
    }
  ],
  "storageKey": null
},
v15 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v16 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "after",
        "variableName": "after"
      },
      {
        "kind": "Variable",
        "name": "first",
        "variableName": "first"
      },
      {
        "kind": "Variable",
        "name": "from",
        "variableName": "from"
      },
      {
        "kind": "Variable",
        "name": "hasEvidence",
        "variableName": "hasEvidence"
      },
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      },
      {
        "kind": "Variable",
        "name": "origin",
        "variableName": "origin"
      },
      {
        "kind": "Variable",
        "name": "to",
        "variableName": "to"
      }
    ],
    "concreteType": "PurchaseConnection",
    "kind": "LinkedField",
    "name": "purchases",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "PurchaseEdge",
        "kind": "LinkedField",
        "name": "edges",
        "plural": true,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "cursor",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "Purchase",
            "kind": "LinkedField",
            "name": "node",
            "plural": false,
            "selections": [
              (v7/*:: as any*/),
              (v8/*:: as any*/),
              (v9/*:: as any*/),
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
                "name": "origin",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "hasEvidence",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "sourceKinds",
                "storageKey": null
              },
              (v10/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": "Account",
                "kind": "LinkedField",
                "name": "paymentAccount",
                "plural": false,
                "selections": [
                  (v7/*:: as any*/),
                  (v11/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "kind",
                    "storageKey": null
                  },
                  (v12/*:: as any*/)
                ],
                "storageKey": null
              },
              (v14/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": "PurchaseItem",
                "kind": "LinkedField",
                "name": "items",
                "plural": true,
                "selections": [
                  (v7/*:: as any*/),
                  (v11/*:: as any*/),
                  (v13/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "quantityNumerator",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "quantityDenominator",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "quantityUnit",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "printedUnitPriceYen",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "printedUnitPriceQuantityNumerator",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "printedUnitPriceQuantityDenominator",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "printedUnitPriceUnit",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "effectiveUnitPriceNumeratorYen",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "effectiveUnitPriceDenominator",
                    "storageKey": null
                  },
                  (v15/*:: as any*/),
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
                    "name": "productVariantId",
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
                      (v12/*:: as any*/),
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
                      (v13/*:: as any*/)
                    ],
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "Tag",
                    "kind": "LinkedField",
                    "name": "tags",
                    "plural": true,
                    "selections": [
                      (v7/*:: as any*/),
                      (v8/*:: as any*/),
                      (v11/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "archived",
                        "storageKey": null
                      },
                      (v15/*:: as any*/)
                    ],
                    "storageKey": null
                  }
                ],
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "totalYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "concreteType": "PurchaseCorrection",
                "kind": "LinkedField",
                "name": "corrections",
                "plural": true,
                "selections": [
                  (v7/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "itemId",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "amountDeltaYen",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "reason",
                    "storageKey": null
                  },
                  (v9/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "settlementMonth",
                    "storageKey": null
                  },
                  (v14/*:: as any*/)
                ],
                "storageKey": null
              },
              (v15/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "status",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "createdAt",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "updatedAt",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "PageInfo",
        "kind": "LinkedField",
        "name": "pageInfo",
        "plural": false,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "endCursor",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "hasNextPage",
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "totalCount",
        "storageKey": null
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v3/*:: as any*/),
      (v4/*:: as any*/),
      (v5/*:: as any*/),
      (v6/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "PurchasesQuery",
    "selections": (v16/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v4/*:: as any*/),
      (v2/*:: as any*/),
      (v6/*:: as any*/),
      (v5/*:: as any*/),
      (v3/*:: as any*/),
      (v0/*:: as any*/),
      (v1/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "PurchasesQuery",
    "selections": (v16/*:: as any*/)
  },
  "params": {
    "cacheID": "d4255ee09952e32f997a00c670fe8a21",
    "id": null,
    "metadata": {},
    "name": "PurchasesQuery",
    "operationKind": "query",
    "text": "query PurchasesQuery(\n  $householdId: ID!\n  $from: String\n  $to: String\n  $origin: PurchaseOrigin\n  $hasEvidence: Boolean\n  $after: String\n  $first: Int\n) {\n  purchases(householdId: $householdId, from: $from, to: $to, origin: $origin, hasEvidence: $hasEvidence, after: $after, first: $first) {\n    edges {\n      cursor\n      node {\n        id\n        householdId\n        occurredAt\n        merchant\n        origin\n        hasEvidence\n        sourceKinds\n        paymentAccountId\n        paymentAccount {\n          id\n          name\n          kind\n          poolId\n        }\n        paymentAllocations {\n          paymentAccountId\n          amountYen\n          account {\n            id\n            name\n          }\n        }\n        items {\n          id\n          name\n          amountYen\n          quantityNumerator\n          quantityDenominator\n          quantityUnit\n          printedUnitPriceYen\n          printedUnitPriceQuantityNumerator\n          printedUnitPriceQuantityDenominator\n          printedUnitPriceUnit\n          effectiveUnitPriceNumeratorYen\n          effectiveUnitPriceDenominator\n          revision\n          categoryId\n          productVariantId\n          allocations {\n            poolId\n            poolName\n            weight\n            amountYen\n          }\n          tags {\n            id\n            householdId\n            name\n            archived\n            revision\n          }\n        }\n        totalYen\n        corrections {\n          id\n          itemId\n          amountDeltaYen\n          reason\n          occurredAt\n          settlementMonth\n          paymentAllocations {\n            paymentAccountId\n            amountYen\n            account {\n              id\n              name\n            }\n          }\n        }\n        revision\n        status\n        createdAt\n        updatedAt\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "477a228a49fcfde8873ffc4e2db064e8";

export default node;

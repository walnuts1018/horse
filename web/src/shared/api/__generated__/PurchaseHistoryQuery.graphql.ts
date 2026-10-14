/**
 * @generated SignedSource<<6535133a3240cf335aea749fa0820468>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseAmountTarget = "ITEM" | "PURCHASE" | "%future added value";
export type PurchaseDatePrecision = "DATE_ONLY" | "DATE_TIME" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
export type PurchaseFilter = {
  amountTarget?: PurchaseAmountTarget | null | undefined;
  categoryId?: string | null | undefined;
  from?: string | null | undefined;
  hasEvidence?: boolean | null | undefined;
  householdId: string;
  maximumAmount?: any | null | undefined;
  minimumAmount?: any | null | undefined;
  origin?: PurchaseOrigin | null | undefined;
  paymentAccountId?: string | null | undefined;
  poolId?: string | null | undefined;
  search?: string | null | undefined;
  tagId?: string | null | undefined;
  to?: string | null | undefined;
  uncategorized?: boolean | null | undefined;
};
export type PurchaseHistoryQuery$variables = {
  after?: string | null | undefined;
  filter: PurchaseFilter;
  first?: number | null | undefined;
};
export type PurchaseHistoryQuery$data = {
  readonly purchases: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly correctionAmountYen: any;
        readonly correctionCount: number;
        readonly createdAt: string;
        readonly datePrecision: PurchaseDatePrecision;
        readonly hasEvidence: boolean;
        readonly householdId: string;
        readonly id: string;
        readonly items: ReadonlyArray<{
          readonly allocations: ReadonlyArray<{
            readonly amountYen: any;
            readonly poolId: string;
            readonly poolName: string;
            readonly weight: any;
          }>;
          readonly amountYen: any;
          readonly categoryId: string | null | undefined;
          readonly currency: string;
          readonly effectiveUnitPrice: {
            readonly denominator: string;
            readonly numeratorYen: string;
          } | null | undefined;
          readonly id: string;
          readonly name: string;
          readonly printedUnitPrice: {
            readonly amountYen: any;
            readonly basis: {
              readonly denominator: string;
              readonly numerator: string;
              readonly unit: string;
            };
          } | null | undefined;
          readonly productVariantId: string | null | undefined;
          readonly quantity: {
            readonly denominator: string;
            readonly numerator: string;
            readonly unit: string;
          } | null | undefined;
          readonly remainingRefundableAmountYen: any;
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
        readonly paymentAllocations: ReadonlyArray<{
          readonly account: {
            readonly id: string;
            readonly name: string;
          };
          readonly amountYen: any;
          readonly paymentAccountId: string;
        }>;
        readonly remainingRefundableAmountYen: any;
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
      readonly hasPreviousPage: boolean;
      readonly startCursor: string | null | undefined;
    };
    readonly syncCursor: string;
    readonly totalCount: number;
  };
};
export type PurchaseHistoryQuery = {
  response: PurchaseHistoryQuery$data;
  variables: PurchaseHistoryQuery$variables;
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
  "name": "filter"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "remainingRefundableAmountYen",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "denominator",
  "storageKey": null
},
v9 = [
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "numerator",
    "storageKey": null
  },
  (v8/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "unit",
    "storageKey": null
  }
],
v10 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v11 = [
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
        "name": "filter",
        "variableName": "filter"
      },
      {
        "kind": "Variable",
        "name": "first",
        "variableName": "first"
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
              (v3/*:: as any*/),
              (v4/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "occurredAt",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "datePrecision",
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
              {
                "alias": null,
                "args": null,
                "concreteType": "PurchasePayment",
                "kind": "LinkedField",
                "name": "paymentAllocations",
                "plural": true,
                "selections": [
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "paymentAccountId",
                    "storageKey": null
                  },
                  (v5/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "Account",
                    "kind": "LinkedField",
                    "name": "account",
                    "plural": false,
                    "selections": [
                      (v3/*:: as any*/),
                      (v6/*:: as any*/)
                    ],
                    "storageKey": null
                  }
                ],
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "concreteType": "PurchaseItem",
                "kind": "LinkedField",
                "name": "items",
                "plural": true,
                "selections": [
                  (v3/*:: as any*/),
                  (v6/*:: as any*/),
                  (v5/*:: as any*/),
                  (v7/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "currency",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "PurchaseQuantity",
                    "kind": "LinkedField",
                    "name": "quantity",
                    "plural": false,
                    "selections": (v9/*:: as any*/),
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "PrintedUnitPrice",
                    "kind": "LinkedField",
                    "name": "printedUnitPrice",
                    "plural": false,
                    "selections": [
                      (v5/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "PurchaseQuantity",
                        "kind": "LinkedField",
                        "name": "basis",
                        "plural": false,
                        "selections": (v9/*:: as any*/),
                        "storageKey": null
                      }
                    ],
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "EffectiveUnitPrice",
                    "kind": "LinkedField",
                    "name": "effectiveUnitPrice",
                    "plural": false,
                    "selections": [
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "numeratorYen",
                        "storageKey": null
                      },
                      (v8/*:: as any*/)
                    ],
                    "storageKey": null
                  },
                  (v10/*:: as any*/),
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
                    "concreteType": "PurchaseItemAllocation",
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
                      (v5/*:: as any*/)
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
                      (v3/*:: as any*/),
                      (v4/*:: as any*/),
                      (v6/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "archived",
                        "storageKey": null
                      },
                      (v10/*:: as any*/)
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
                "kind": "ScalarField",
                "name": "correctionAmountYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "correctionCount",
                "storageKey": null
              },
              (v7/*:: as any*/),
              (v10/*:: as any*/),
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
            "name": "startCursor",
            "storageKey": null
          },
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
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "hasPreviousPage",
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
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "syncCursor",
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
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "PurchaseHistoryQuery",
    "selections": (v11/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "PurchaseHistoryQuery",
    "selections": (v11/*:: as any*/)
  },
  "params": {
    "cacheID": "5bd2a042e1bb09b9baa3f8042c7ce76a",
    "id": null,
    "metadata": {},
    "name": "PurchaseHistoryQuery",
    "operationKind": "query",
    "text": "query PurchaseHistoryQuery(\n  $filter: PurchaseFilter!\n  $first: Int\n  $after: String\n) {\n  purchases(filter: $filter, after: $after, first: $first) {\n    edges {\n      cursor\n      node {\n        id\n        householdId\n        occurredAt\n        datePrecision\n        merchant\n        origin\n        hasEvidence\n        sourceKinds\n        paymentAllocations {\n          paymentAccountId\n          amountYen\n          account {\n            id\n            name\n          }\n        }\n        items {\n          id\n          name\n          amountYen\n          remainingRefundableAmountYen\n          currency\n          quantity {\n            numerator\n            denominator\n            unit\n          }\n          printedUnitPrice {\n            amountYen\n            basis {\n              numerator\n              denominator\n              unit\n            }\n          }\n          effectiveUnitPrice {\n            numeratorYen\n            denominator\n          }\n          revision\n          categoryId\n          productVariantId\n          allocations {\n            poolId\n            poolName\n            weight\n            amountYen\n          }\n          tags {\n            id\n            householdId\n            name\n            archived\n            revision\n          }\n        }\n        totalYen\n        correctionAmountYen\n        correctionCount\n        remainingRefundableAmountYen\n        revision\n        status\n        createdAt\n        updatedAt\n      }\n    }\n    pageInfo {\n      startCursor\n      endCursor\n      hasNextPage\n      hasPreviousPage\n    }\n    totalCount\n    syncCursor\n  }\n}\n"
  }
};
})();

(node as any).hash = "13ef77ddc54026b9d16d7b96c7948c09";

export default node;

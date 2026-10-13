/**
 * @generated SignedSource<<73a9b3b2356aa24317e3704f08602907>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type PurchaseAmountTarget = "ITEM" | "PURCHASE" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
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
export type PurchasesQuery$variables = {
  filter: PurchaseFilter;
  first?: number | null | undefined;
};
export type PurchasesQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"PurchaseListFragment">;
};
export type PurchasesQuery = {
  response: PurchasesQuery$data;
  variables: PurchasesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "filter"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "first"
  }
],
v1 = [
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
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "cursor",
  "storageKey": null
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
  "name": "occurredAt",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v8 = {
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
    (v6/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "concreteType": "Account",
      "kind": "LinkedField",
      "name": "account",
      "plural": false,
      "selections": [
        (v3/*:: as any*/),
        (v7/*:: as any*/)
      ],
      "storageKey": null
    }
  ],
  "storageKey": null
},
v9 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "denominator",
  "storageKey": null
},
v10 = [
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "numerator",
    "storageKey": null
  },
  (v9/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "unit",
    "storageKey": null
  }
],
v11 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v12 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v13 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "endCursor",
  "storageKey": null
},
v14 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "hasNextPage",
  "storageKey": null
};
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "PurchasesQuery",
    "selections": [
      {
        "args": (v1/*:: as any*/),
        "kind": "FragmentSpread",
        "name": "PurchaseListFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "PurchasesQuery",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
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
              (v2/*:: as any*/),
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
                  (v5/*:: as any*/),
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
                  (v8/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "PurchaseItem",
                    "kind": "LinkedField",
                    "name": "items",
                    "plural": true,
                    "selections": [
                      (v3/*:: as any*/),
                      (v7/*:: as any*/),
                      (v6/*:: as any*/),
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
                        "selections": (v10/*:: as any*/),
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
                          (v6/*:: as any*/),
                          {
                            "alias": null,
                            "args": null,
                            "concreteType": "PurchaseQuantity",
                            "kind": "LinkedField",
                            "name": "basis",
                            "plural": false,
                            "selections": (v10/*:: as any*/),
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
                          (v9/*:: as any*/)
                        ],
                        "storageKey": null
                      },
                      (v11/*:: as any*/),
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
                          (v6/*:: as any*/)
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
                          (v7/*:: as any*/),
                          {
                            "alias": null,
                            "args": null,
                            "kind": "ScalarField",
                            "name": "archived",
                            "storageKey": null
                          },
                          (v11/*:: as any*/)
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
                    "args": [
                      {
                        "kind": "Literal",
                        "name": "first",
                        "value": 100
                      }
                    ],
                    "concreteType": "PurchaseCorrectionConnection",
                    "kind": "LinkedField",
                    "name": "corrections",
                    "plural": false,
                    "selections": [
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "PurchaseCorrectionEdge",
                        "kind": "LinkedField",
                        "name": "edges",
                        "plural": true,
                        "selections": [
                          (v2/*:: as any*/),
                          {
                            "alias": null,
                            "args": null,
                            "concreteType": null,
                            "kind": "LinkedField",
                            "name": "node",
                            "plural": false,
                            "selections": [
                              (v12/*:: as any*/),
                              (v3/*:: as any*/),
                              {
                                "kind": "InlineFragment",
                                "selections": [
                                  {
                                    "alias": null,
                                    "args": null,
                                    "kind": "ScalarField",
                                    "name": "itemId",
                                    "storageKey": null
                                  }
                                ],
                                "type": "PurchaseItemCorrection",
                                "abstractKey": null
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
                              (v5/*:: as any*/),
                              {
                                "alias": null,
                                "args": null,
                                "kind": "ScalarField",
                                "name": "settlementMonth",
                                "storageKey": null
                              },
                              (v8/*:: as any*/)
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
                          (v13/*:: as any*/),
                          (v14/*:: as any*/)
                        ],
                        "storageKey": null
                      }
                    ],
                    "storageKey": "corrections(first:100)"
                  },
                  (v11/*:: as any*/),
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
                  },
                  (v12/*:: as any*/)
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
              (v13/*:: as any*/),
              (v14/*:: as any*/),
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
      },
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "filters": [
          "filter"
        ],
        "handle": "connection",
        "key": "PurchaseListFragment_purchases",
        "kind": "LinkedHandle",
        "name": "purchases"
      }
    ]
  },
  "params": {
    "cacheID": "774ab305e3ec4ac412c538e1c3afa694",
    "id": null,
    "metadata": {},
    "name": "PurchasesQuery",
    "operationKind": "query",
    "text": "query PurchasesQuery(\n  $filter: PurchaseFilter!\n  $first: Int\n) {\n  ...PurchaseListFragment_1qzYLD\n}\n\nfragment PurchaseListFragment_1qzYLD on Query {\n  purchases(filter: $filter, first: $first) {\n    edges {\n      cursor\n      node {\n        id\n        householdId\n        occurredAt\n        merchant\n        origin\n        hasEvidence\n        sourceKinds\n        paymentAllocations {\n          paymentAccountId\n          amountYen\n          account {\n            id\n            name\n          }\n        }\n        items {\n          id\n          name\n          amountYen\n          currency\n          quantity {\n            numerator\n            denominator\n            unit\n          }\n          printedUnitPrice {\n            amountYen\n            basis {\n              numerator\n              denominator\n              unit\n            }\n          }\n          effectiveUnitPrice {\n            numeratorYen\n            denominator\n          }\n          revision\n          categoryId\n          productVariantId\n          allocations {\n            poolId\n            poolName\n            weight\n            amountYen\n          }\n          tags {\n            id\n            householdId\n            name\n            archived\n            revision\n          }\n        }\n        totalYen\n        corrections(first: 100) {\n          edges {\n            cursor\n            node {\n              __typename\n              id\n              ... on PurchaseItemCorrection {\n                itemId\n              }\n              amountDeltaYen\n              reason\n              occurredAt\n              settlementMonth\n              paymentAllocations {\n                paymentAccountId\n                amountYen\n                account {\n                  id\n                  name\n                }\n              }\n            }\n          }\n          pageInfo {\n            endCursor\n            hasNextPage\n          }\n        }\n        revision\n        status\n        createdAt\n        updatedAt\n        __typename\n      }\n    }\n    pageInfo {\n      startCursor\n      endCursor\n      hasNextPage\n      hasPreviousPage\n    }\n    totalCount\n    syncCursor\n  }\n}\n"
  }
};
})();

(node as any).hash = "3b913431581b17bf5f0bb79b37ecc1f0";

export default node;

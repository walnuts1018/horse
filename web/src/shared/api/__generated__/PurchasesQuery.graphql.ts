/**
 * @generated SignedSource<<f41de0db7bb75f36bb12c664caae39dc>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type PurchaseAmountTarget = "ITEM" | "PURCHASE" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
export type PurchasesQuery$variables = {
  amountTarget?: PurchaseAmountTarget | null | undefined;
  categoryId?: string | null | undefined;
  first?: number | null | undefined;
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
export type PurchasesQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"PurchaseListFragment">;
};
export type PurchasesQuery = {
  response: PurchasesQuery$data;
  variables: PurchasesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "amountTarget"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "categoryId"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "from"
},
v4 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "hasEvidence"
},
v5 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v6 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "maximumAmount"
},
v7 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "minimumAmount"
},
v8 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "origin"
},
v9 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "paymentAccountId"
},
v10 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "poolId"
},
v11 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "search"
},
v12 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "tagId"
},
v13 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v14 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "uncategorized"
},
v15 = [
  {
    "kind": "Variable",
    "name": "amountTarget",
    "variableName": "amountTarget"
  },
  {
    "kind": "Variable",
    "name": "categoryId",
    "variableName": "categoryId"
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
    "name": "maximumAmount",
    "variableName": "maximumAmount"
  },
  {
    "kind": "Variable",
    "name": "minimumAmount",
    "variableName": "minimumAmount"
  },
  {
    "kind": "Variable",
    "name": "origin",
    "variableName": "origin"
  },
  {
    "kind": "Variable",
    "name": "paymentAccountId",
    "variableName": "paymentAccountId"
  },
  {
    "kind": "Variable",
    "name": "poolId",
    "variableName": "poolId"
  },
  {
    "kind": "Variable",
    "name": "search",
    "variableName": "search"
  },
  {
    "kind": "Variable",
    "name": "tagId",
    "variableName": "tagId"
  },
  {
    "kind": "Variable",
    "name": "to",
    "variableName": "to"
  },
  {
    "kind": "Variable",
    "name": "uncategorized",
    "variableName": "uncategorized"
  }
],
v16 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "cursor",
  "storageKey": null
},
v17 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v18 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v19 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
},
v20 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v21 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v22 = {
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
    (v20/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "concreteType": "Account",
      "kind": "LinkedField",
      "name": "account",
      "plural": false,
      "selections": [
        (v17/*:: as any*/),
        (v21/*:: as any*/)
      ],
      "storageKey": null
    }
  ],
  "storageKey": null
},
v23 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "denominator",
  "storageKey": null
},
v24 = [
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "numerator",
    "storageKey": null
  },
  (v23/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "unit",
    "storageKey": null
  }
],
v25 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v26 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v27 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "endCursor",
  "storageKey": null
},
v28 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "hasNextPage",
  "storageKey": null
};
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v3/*:: as any*/),
      (v4/*:: as any*/),
      (v5/*:: as any*/),
      (v6/*:: as any*/),
      (v7/*:: as any*/),
      (v8/*:: as any*/),
      (v9/*:: as any*/),
      (v10/*:: as any*/),
      (v11/*:: as any*/),
      (v12/*:: as any*/),
      (v13/*:: as any*/),
      (v14/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "PurchasesQuery",
    "selections": [
      {
        "args": (v15/*:: as any*/),
        "kind": "FragmentSpread",
        "name": "PurchaseListFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v5/*:: as any*/),
      (v3/*:: as any*/),
      (v13/*:: as any*/),
      (v8/*:: as any*/),
      (v4/*:: as any*/),
      (v11/*:: as any*/),
      (v1/*:: as any*/),
      (v14/*:: as any*/),
      (v12/*:: as any*/),
      (v9/*:: as any*/),
      (v10/*:: as any*/),
      (v7/*:: as any*/),
      (v6/*:: as any*/),
      (v0/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "PurchasesQuery",
    "selections": [
      {
        "alias": null,
        "args": (v15/*:: as any*/),
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
              (v16/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": "Purchase",
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
                "selections": [
                  (v17/*:: as any*/),
                  (v18/*:: as any*/),
                  (v19/*:: as any*/),
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
                  (v22/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "PurchaseItem",
                    "kind": "LinkedField",
                    "name": "items",
                    "plural": true,
                    "selections": [
                      (v17/*:: as any*/),
                      (v21/*:: as any*/),
                      (v20/*:: as any*/),
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
                        "selections": (v24/*:: as any*/),
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
                          (v20/*:: as any*/),
                          {
                            "alias": null,
                            "args": null,
                            "concreteType": "PurchaseQuantity",
                            "kind": "LinkedField",
                            "name": "basis",
                            "plural": false,
                            "selections": (v24/*:: as any*/),
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
                          (v23/*:: as any*/)
                        ],
                        "storageKey": null
                      },
                      (v25/*:: as any*/),
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
                          (v20/*:: as any*/)
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
                          (v17/*:: as any*/),
                          (v18/*:: as any*/),
                          (v21/*:: as any*/),
                          {
                            "alias": null,
                            "args": null,
                            "kind": "ScalarField",
                            "name": "archived",
                            "storageKey": null
                          },
                          (v25/*:: as any*/)
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
                          (v16/*:: as any*/),
                          {
                            "alias": null,
                            "args": null,
                            "concreteType": null,
                            "kind": "LinkedField",
                            "name": "node",
                            "plural": false,
                            "selections": [
                              (v26/*:: as any*/),
                              (v17/*:: as any*/),
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
                              (v19/*:: as any*/),
                              {
                                "alias": null,
                                "args": null,
                                "kind": "ScalarField",
                                "name": "settlementMonth",
                                "storageKey": null
                              },
                              (v22/*:: as any*/)
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
                          (v27/*:: as any*/),
                          (v28/*:: as any*/)
                        ],
                        "storageKey": null
                      }
                    ],
                    "storageKey": "corrections(first:100)"
                  },
                  (v25/*:: as any*/),
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
                  (v26/*:: as any*/)
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
              (v27/*:: as any*/),
              (v28/*:: as any*/),
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
        "args": (v15/*:: as any*/),
        "filters": [
          "householdId",
          "from",
          "to",
          "origin",
          "hasEvidence",
          "search",
          "categoryId",
          "uncategorized",
          "tagId",
          "paymentAccountId",
          "poolId",
          "minimumAmount",
          "maximumAmount",
          "amountTarget"
        ],
        "handle": "connection",
        "key": "PurchaseListFragment_purchases",
        "kind": "LinkedHandle",
        "name": "purchases"
      }
    ]
  },
  "params": {
    "cacheID": "8f50a3032ef83b1ba61e31b574af1d76",
    "id": null,
    "metadata": {},
    "name": "PurchasesQuery",
    "operationKind": "query",
    "text": "query PurchasesQuery(\n  $householdId: ID!\n  $from: DateTime\n  $to: DateTime\n  $origin: PurchaseOrigin\n  $hasEvidence: Boolean\n  $search: String\n  $categoryId: ID\n  $uncategorized: Boolean\n  $tagId: ID\n  $paymentAccountId: ID\n  $poolId: ID\n  $minimumAmount: Long\n  $maximumAmount: Long\n  $amountTarget: PurchaseAmountTarget\n  $first: Int\n) {\n  ...PurchaseListFragment_o8fIU\n}\n\nfragment PurchaseListFragment_o8fIU on Query {\n  purchases(householdId: $householdId, from: $from, to: $to, origin: $origin, hasEvidence: $hasEvidence, search: $search, categoryId: $categoryId, uncategorized: $uncategorized, tagId: $tagId, paymentAccountId: $paymentAccountId, poolId: $poolId, minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, amountTarget: $amountTarget, first: $first) {\n    edges {\n      cursor\n      node {\n        id\n        householdId\n        occurredAt\n        merchant\n        origin\n        hasEvidence\n        sourceKinds\n        paymentAllocations {\n          paymentAccountId\n          amountYen\n          account {\n            id\n            name\n          }\n        }\n        items {\n          id\n          name\n          amountYen\n          currency\n          quantity {\n            numerator\n            denominator\n            unit\n          }\n          printedUnitPrice {\n            amountYen\n            basis {\n              numerator\n              denominator\n              unit\n            }\n          }\n          effectiveUnitPrice {\n            numeratorYen\n            denominator\n          }\n          revision\n          categoryId\n          productVariantId\n          allocations {\n            poolId\n            poolName\n            weight\n            amountYen\n          }\n          tags {\n            id\n            householdId\n            name\n            archived\n            revision\n          }\n        }\n        totalYen\n        corrections(first: 100) {\n          edges {\n            cursor\n            node {\n              __typename\n              id\n              ... on PurchaseItemCorrection {\n                itemId\n              }\n              amountDeltaYen\n              reason\n              occurredAt\n              settlementMonth\n              paymentAllocations {\n                paymentAccountId\n                amountYen\n                account {\n                  id\n                  name\n                }\n              }\n            }\n          }\n          pageInfo {\n            endCursor\n            hasNextPage\n          }\n        }\n        revision\n        status\n        createdAt\n        updatedAt\n        __typename\n      }\n    }\n    pageInfo {\n      startCursor\n      endCursor\n      hasNextPage\n      hasPreviousPage\n    }\n    totalCount\n    syncCursor\n  }\n}\n"
  }
};
})();

(node as any).hash = "bccf536e7920feab335871adf27b8a6f";

export default node;

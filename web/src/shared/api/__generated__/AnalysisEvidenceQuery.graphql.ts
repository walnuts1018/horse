/**
 * @generated SignedSource<<fd46ab3e718b7885573149cf7d339147>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type PurchaseAmountTarget = "ITEM" | "PURCHASE" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
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
  status?: PurchaseStatus | null | undefined;
  tagId?: string | null | undefined;
  to?: string | null | undefined;
  uncategorized?: boolean | null | undefined;
};
export type AnalysisEvidenceQuery$variables = {
  from: string;
  householdId: string;
  purchaseFilter: PurchaseFilter;
  to: string;
};
export type AnalysisEvidenceQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisUnbackedPurchasesFragment" | "AnalysisUnmatchedObservationsFragment">;
};
export type AnalysisEvidenceQuery = {
  response: AnalysisEvidenceQuery$data;
  variables: AnalysisEvidenceQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "from"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "purchaseFilter"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v4 = {
  "kind": "Variable",
  "name": "filter",
  "variableName": "purchaseFilter"
},
v5 = {
  "kind": "Variable",
  "name": "from",
  "variableName": "from"
},
v6 = {
  "kind": "Variable",
  "name": "householdId",
  "variableName": "householdId"
},
v7 = {
  "kind": "Variable",
  "name": "to",
  "variableName": "to"
},
v8 = {
  "kind": "Literal",
  "name": "first",
  "value": 8
},
v9 = [
  (v4/*:: as any*/),
  (v8/*:: as any*/)
],
v10 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v11 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
},
v12 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "merchant",
  "storageKey": null
},
v13 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v14 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "cursor",
  "storageKey": null
},
v15 = {
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
v16 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "totalCount",
  "storageKey": null
},
v17 = [
  (v8/*:: as any*/),
  (v5/*:: as any*/),
  (v6/*:: as any*/),
  {
    "kind": "Literal",
    "name": "state",
    "value": "UNMATCHED"
  },
  (v7/*:: as any*/)
];
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v3/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "AnalysisEvidenceQuery",
    "selections": [
      {
        "args": [
          (v4/*:: as any*/)
        ],
        "kind": "FragmentSpread",
        "name": "AnalysisUnbackedPurchasesFragment"
      },
      {
        "args": [
          (v5/*:: as any*/),
          (v6/*:: as any*/),
          (v7/*:: as any*/)
        ],
        "kind": "FragmentSpread",
        "name": "AnalysisUnmatchedObservationsFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/),
      (v3/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "AnalysisEvidenceQuery",
    "selections": [
      {
        "alias": null,
        "args": (v9/*:: as any*/),
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
                "concreteType": "Purchase",
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
                "selections": [
                  (v10/*:: as any*/),
                  (v11/*:: as any*/),
                  (v12/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "totalYen",
                    "storageKey": null
                  },
                  (v13/*:: as any*/)
                ],
                "storageKey": null
              },
              (v14/*:: as any*/)
            ],
            "storageKey": null
          },
          (v15/*:: as any*/),
          (v16/*:: as any*/)
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": (v9/*:: as any*/),
        "filters": [
          "filter"
        ],
        "handle": "connection",
        "key": "AnalysisUnbackedPurchasesFragment_purchases",
        "kind": "LinkedHandle",
        "name": "purchases"
      },
      {
        "alias": null,
        "args": (v17/*:: as any*/),
        "concreteType": "UnmatchedObservationConnection",
        "kind": "LinkedField",
        "name": "unmatchedObservations",
        "plural": false,
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "UnmatchedObservationEdge",
            "kind": "LinkedField",
            "name": "edges",
            "plural": true,
            "selections": [
              {
                "alias": null,
                "args": null,
                "concreteType": null,
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
                "selections": [
                  (v13/*:: as any*/),
                  (v10/*:: as any*/),
                  {
                    "kind": "InlineFragment",
                    "selections": [
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "sourceKind",
                        "storageKey": null
                      },
                      (v12/*:: as any*/),
                      (v11/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "amountYen",
                        "storageKey": null
                      },
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "orderId",
                        "storageKey": null
                      },
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "transactionId",
                        "storageKey": null
                      }
                    ],
                    "type": "UnmatchedObservation",
                    "abstractKey": null
                  }
                ],
                "storageKey": null
              },
              (v14/*:: as any*/)
            ],
            "storageKey": null
          },
          (v15/*:: as any*/),
          (v16/*:: as any*/)
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": (v17/*:: as any*/),
        "filters": [
          "householdId",
          "from",
          "to"
        ],
        "handle": "connection",
        "key": "AnalysisUnmatchedObservationsFragment_unmatchedObservations",
        "kind": "LinkedHandle",
        "name": "unmatchedObservations"
      }
    ]
  },
  "params": {
    "cacheID": "d5706e2b869f85868fe90055da6a1ae3",
    "id": null,
    "metadata": {},
    "name": "AnalysisEvidenceQuery",
    "operationKind": "query",
    "text": "query AnalysisEvidenceQuery(\n  $householdId: ID!\n  $from: YearMonth!\n  $to: YearMonth!\n  $purchaseFilter: PurchaseFilter!\n) {\n  ...AnalysisUnbackedPurchasesFragment_i4QhS\n  ...AnalysisUnmatchedObservationsFragment_2M8lQq\n}\n\nfragment AnalysisUnbackedPurchasesFragment_i4QhS on Query {\n  purchases(filter: $purchaseFilter, first: 8) {\n    edges {\n      node {\n        id\n        occurredAt\n        merchant\n        totalYen\n        __typename\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n\nfragment AnalysisUnmatchedObservationsFragment_2M8lQq on Query {\n  unmatchedObservations(householdId: $householdId, from: $from, to: $to, state: UNMATCHED, first: 8) {\n    edges {\n      node {\n        __typename\n        id\n        ... on UnmatchedObservation {\n          sourceKind\n          merchant\n          occurredAt\n          amountYen\n          orderId\n          transactionId\n        }\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "3885b6c2042999e04aa1a125b3bd31f4";

export default node;

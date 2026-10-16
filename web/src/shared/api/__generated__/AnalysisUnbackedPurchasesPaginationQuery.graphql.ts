/**
 * @generated SignedSource<<5c5a99530883bcccf96556200a7d3555>>
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
export type AnalysisUnbackedPurchasesPaginationQuery$variables = {
  after?: string | null | undefined;
  filter: PurchaseFilter;
  first?: number | null | undefined;
};
export type AnalysisUnbackedPurchasesPaginationQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisUnbackedPurchasesFragment">;
};
export type AnalysisUnbackedPurchasesPaginationQuery = {
  response: AnalysisUnbackedPurchasesPaginationQuery$data;
  variables: AnalysisUnbackedPurchasesPaginationQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "after"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "filter"
  },
  {
    "defaultValue": 8,
    "kind": "LocalArgument",
    "name": "first"
  }
],
v1 = [
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
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "AnalysisUnbackedPurchasesPaginationQuery",
    "selections": [
      {
        "args": (v1/*:: as any*/),
        "kind": "FragmentSpread",
        "name": "AnalysisUnbackedPurchasesFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AnalysisUnbackedPurchasesPaginationQuery",
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
              {
                "alias": null,
                "args": null,
                "concreteType": "Purchase",
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
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
                    "name": "occurredAt",
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
                    "name": "totalYen",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "__typename",
                    "storageKey": null
                  }
                ],
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "cursor",
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
      },
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "filters": [
          "filter"
        ],
        "handle": "connection",
        "key": "AnalysisUnbackedPurchasesFragment_purchases",
        "kind": "LinkedHandle",
        "name": "purchases"
      }
    ]
  },
  "params": {
    "cacheID": "21f541b12cd2dfc2c49ea4167810c745",
    "id": null,
    "metadata": {},
    "name": "AnalysisUnbackedPurchasesPaginationQuery",
    "operationKind": "query",
    "text": "query AnalysisUnbackedPurchasesPaginationQuery(\n  $after: String\n  $filter: PurchaseFilter!\n  $first: Int = 8\n) {\n  ...AnalysisUnbackedPurchasesFragment_G9cLv\n}\n\nfragment AnalysisUnbackedPurchasesFragment_G9cLv on Query {\n  purchases(filter: $filter, first: $first, after: $after) {\n    edges {\n      node {\n        id\n        occurredAt\n        merchant\n        totalYen\n        __typename\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "3949d0679800466fcbaf9dd09127d8ce";

export default node;

/**
 * @generated SignedSource<<e56ac610d1d8792f4b2bc97de2994055>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type AnalysisUnmatchedObservationsPaginationQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  from: string;
  householdId: string;
  to: string;
};
export type AnalysisUnmatchedObservationsPaginationQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisUnmatchedObservationsFragment">;
};
export type AnalysisUnmatchedObservationsPaginationQuery = {
  response: AnalysisUnmatchedObservationsPaginationQuery$data;
  variables: AnalysisUnmatchedObservationsPaginationQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "after"
  },
  {
    "defaultValue": 8,
    "kind": "LocalArgument",
    "name": "first"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "from"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "to"
  }
],
v1 = {
  "kind": "Variable",
  "name": "after",
  "variableName": "after"
},
v2 = {
  "kind": "Variable",
  "name": "first",
  "variableName": "first"
},
v3 = {
  "kind": "Variable",
  "name": "from",
  "variableName": "from"
},
v4 = {
  "kind": "Variable",
  "name": "householdId",
  "variableName": "householdId"
},
v5 = {
  "kind": "Variable",
  "name": "to",
  "variableName": "to"
},
v6 = [
  (v1/*:: as any*/),
  (v2/*:: as any*/),
  (v3/*:: as any*/),
  (v4/*:: as any*/),
  {
    "kind": "Literal",
    "name": "state",
    "value": "UNMATCHED"
  },
  (v5/*:: as any*/)
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "AnalysisUnmatchedObservationsPaginationQuery",
    "selections": [
      {
        "args": [
          (v1/*:: as any*/),
          (v2/*:: as any*/),
          (v3/*:: as any*/),
          (v4/*:: as any*/),
          (v5/*:: as any*/)
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
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AnalysisUnmatchedObservationsPaginationQuery",
    "selections": [
      {
        "alias": null,
        "args": (v6/*:: as any*/),
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
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "__typename",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "id",
                    "storageKey": null
                  },
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
                        "name": "occurredAt",
                        "storageKey": null
                      },
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
        "args": (v6/*:: as any*/),
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
    "cacheID": "0ba914893170761fb124106f5918e1df",
    "id": null,
    "metadata": {},
    "name": "AnalysisUnmatchedObservationsPaginationQuery",
    "operationKind": "query",
    "text": "query AnalysisUnmatchedObservationsPaginationQuery(\n  $after: String\n  $first: Int = 8\n  $from: YearMonth!\n  $householdId: ID!\n  $to: YearMonth!\n) {\n  ...AnalysisUnmatchedObservationsFragment_1vFOCd\n}\n\nfragment AnalysisUnmatchedObservationsFragment_1vFOCd on Query {\n  unmatchedObservations(householdId: $householdId, from: $from, to: $to, state: UNMATCHED, first: $first, after: $after) {\n    edges {\n      node {\n        __typename\n        id\n        ... on UnmatchedObservation {\n          sourceKind\n          merchant\n          occurredAt\n          amountYen\n          orderId\n          transactionId\n        }\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "c1bec1a5ce3c923cff05f5053e3a1340";

export default node;

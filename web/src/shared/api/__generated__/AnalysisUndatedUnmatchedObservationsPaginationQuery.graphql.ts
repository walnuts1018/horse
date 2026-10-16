/**
 * @generated SignedSource<<f343a7ecf4da607f2ea3bbfeb35c04bc>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type AnalysisUndatedUnmatchedObservationsPaginationQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type AnalysisUndatedUnmatchedObservationsPaginationQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisUndatedUnmatchedObservationsFragment">;
};
export type AnalysisUndatedUnmatchedObservationsPaginationQuery = {
  response: AnalysisUndatedUnmatchedObservationsPaginationQuery$data;
  variables: AnalysisUndatedUnmatchedObservationsPaginationQuery$variables;
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
    "name": "householdId"
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
    "name": "first",
    "variableName": "first"
  },
  {
    "kind": "Variable",
    "name": "householdId",
    "variableName": "householdId"
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "AnalysisUndatedUnmatchedObservationsPaginationQuery",
    "selections": [
      {
        "args": (v1/*:: as any*/),
        "kind": "FragmentSpread",
        "name": "AnalysisUndatedUnmatchedObservationsFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AnalysisUndatedUnmatchedObservationsPaginationQuery",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": "UnmatchedObservationConnection",
        "kind": "LinkedField",
        "name": "undatedUnmatchedObservations",
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
        "args": (v1/*:: as any*/),
        "filters": [
          "householdId"
        ],
        "handle": "connection",
        "key": "AnalysisUndatedUnmatchedObservationsFragment_undatedUnmatchedObservations",
        "kind": "LinkedHandle",
        "name": "undatedUnmatchedObservations"
      }
    ]
  },
  "params": {
    "cacheID": "8bdcc03c9427db78b8d0994f9b7f4fc5",
    "id": null,
    "metadata": {},
    "name": "AnalysisUndatedUnmatchedObservationsPaginationQuery",
    "operationKind": "query",
    "text": "query AnalysisUndatedUnmatchedObservationsPaginationQuery(\n  $after: String\n  $first: Int = 8\n  $householdId: ID!\n) {\n  ...AnalysisUndatedUnmatchedObservationsFragment_1x5y0e\n}\n\nfragment AnalysisUndatedUnmatchedObservationsFragment_1x5y0e on Query {\n  undatedUnmatchedObservations(householdId: $householdId, first: $first, after: $after) {\n    edges {\n      node {\n        __typename\n        id\n        ... on UnmatchedObservation {\n          sourceKind\n          merchant\n          amountYen\n          orderId\n          transactionId\n        }\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "5ef1b23b0130155a37c7006826968b25";

export default node;

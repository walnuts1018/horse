/**
 * @generated SignedSource<<417ee5a79855927094bae4b48dbc46cf>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
export type UnmatchedObservationState = "CONFIRMED" | "UNMATCHED" | "%future added value";
export type UnmatchedObservationsPaginationQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  from?: string | null | undefined;
  householdId: string;
  sourceKind?: SourceKind | null | undefined;
  state?: UnmatchedObservationState | null | undefined;
  to?: string | null | undefined;
};
export type UnmatchedObservationsPaginationQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"UnmatchedObservationsFragment">;
};
export type UnmatchedObservationsPaginationQuery = {
  response: UnmatchedObservationsPaginationQuery$data;
  variables: UnmatchedObservationsPaginationQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "after"
  },
  {
    "defaultValue": 50,
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
    "name": "sourceKind"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "state"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "to"
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
    "name": "from",
    "variableName": "from"
  },
  {
    "kind": "Variable",
    "name": "householdId",
    "variableName": "householdId"
  },
  {
    "kind": "Variable",
    "name": "sourceKind",
    "variableName": "sourceKind"
  },
  {
    "kind": "Variable",
    "name": "state",
    "variableName": "state"
  },
  {
    "kind": "Variable",
    "name": "to",
    "variableName": "to"
  }
],
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "merchant",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
};
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "UnmatchedObservationsPaginationQuery",
    "selections": [
      {
        "args": (v1/*:: as any*/),
        "kind": "FragmentSpread",
        "name": "UnmatchedObservationsFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "UnmatchedObservationsPaginationQuery",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
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
                "kind": "ScalarField",
                "name": "cursor",
                "storageKey": null
              },
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
                    "name": "documentId",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "sourceReference",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "sourceKind",
                    "storageKey": null
                  },
                  (v2/*:: as any*/),
                  (v3/*:: as any*/),
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
                  },
                  {
                    "kind": "InlineFragment",
                    "selections": [
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "Purchase",
                        "kind": "LinkedField",
                        "name": "purchase",
                        "plural": false,
                        "selections": [
                          (v4/*:: as any*/),
                          (v2/*:: as any*/),
                          (v3/*:: as any*/)
                        ],
                        "storageKey": null
                      }
                    ],
                    "type": "ConfirmedObservation",
                    "abstractKey": null
                  },
                  (v4/*:: as any*/)
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
      },
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "filters": [
          "householdId",
          "from",
          "to",
          "sourceKind",
          "state"
        ],
        "handle": "connection",
        "key": "UnmatchedObservationsFragment_unmatchedObservations",
        "kind": "LinkedHandle",
        "name": "unmatchedObservations"
      }
    ]
  },
  "params": {
    "cacheID": "97296ca7e1dee113b9a5db96e71ee383",
    "id": null,
    "metadata": {},
    "name": "UnmatchedObservationsPaginationQuery",
    "operationKind": "query",
    "text": "query UnmatchedObservationsPaginationQuery(\n  $after: String\n  $first: Int = 50\n  $from: YearMonth\n  $householdId: ID!\n  $sourceKind: SourceKind\n  $state: UnmatchedObservationState\n  $to: YearMonth\n) {\n  ...UnmatchedObservationsFragment_2SLxW\n}\n\nfragment UnmatchedObservationsFragment_2SLxW on Query {\n  unmatchedObservations(householdId: $householdId, from: $from, to: $to, sourceKind: $sourceKind, state: $state, after: $after, first: $first) {\n    edges {\n      cursor\n      node {\n        __typename\n        documentId\n        sourceReference\n        sourceKind\n        merchant\n        occurredAt\n        amountYen\n        orderId\n        transactionId\n        ... on ConfirmedObservation {\n          purchase {\n            id\n            merchant\n            occurredAt\n          }\n        }\n        id\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "d73baa0994d486a7a2aceeba30ff9cb1";

export default node;

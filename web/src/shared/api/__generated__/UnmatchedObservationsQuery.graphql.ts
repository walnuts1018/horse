/**
 * @generated SignedSource<<6fbd8e6c10370372f20daf7944a3955f>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
export type UnmatchedObservationState = "CONFIRMED" | "UNMATCHED" | "%future added value";
export type UnmatchedObservationsQuery$variables = {
  first?: number | null | undefined;
  from?: string | null | undefined;
  householdId: string;
  sourceKind?: SourceKind | null | undefined;
  state?: UnmatchedObservationState | null | undefined;
  to?: string | null | undefined;
};
export type UnmatchedObservationsQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"UnmatchedObservationsFragment">;
};
export type UnmatchedObservationsQuery = {
  response: UnmatchedObservationsQuery$data;
  variables: UnmatchedObservationsQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": 50,
  "kind": "LocalArgument",
  "name": "first"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "from"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "sourceKind"
},
v4 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "state"
},
v5 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v6 = [
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
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "merchant",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
},
v9 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
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
      (v5/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "UnmatchedObservationsQuery",
    "selections": [
      {
        "args": (v6/*:: as any*/),
        "kind": "FragmentSpread",
        "name": "UnmatchedObservationsFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v1/*:: as any*/),
      (v5/*:: as any*/),
      (v3/*:: as any*/),
      (v4/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "UnmatchedObservationsQuery",
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
                  (v7/*:: as any*/),
                  (v8/*:: as any*/),
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
                          (v9/*:: as any*/),
                          (v7/*:: as any*/),
                          (v8/*:: as any*/)
                        ],
                        "storageKey": null
                      }
                    ],
                    "type": "ConfirmedObservation",
                    "abstractKey": null
                  },
                  (v9/*:: as any*/)
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
        "args": (v6/*:: as any*/),
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
    "cacheID": "bdea0b93d183b1c0b8201c8903f84949",
    "id": null,
    "metadata": {},
    "name": "UnmatchedObservationsQuery",
    "operationKind": "query",
    "text": "query UnmatchedObservationsQuery(\n  $householdId: ID!\n  $from: YearMonth\n  $to: YearMonth\n  $sourceKind: SourceKind\n  $state: UnmatchedObservationState\n  $first: Int = 50\n) {\n  ...UnmatchedObservationsFragment_4Eka4U\n}\n\nfragment UnmatchedObservationsFragment_4Eka4U on Query {\n  unmatchedObservations(householdId: $householdId, from: $from, to: $to, sourceKind: $sourceKind, state: $state, first: $first) {\n    edges {\n      cursor\n      node {\n        __typename\n        documentId\n        sourceReference\n        sourceKind\n        merchant\n        occurredAt\n        amountYen\n        orderId\n        transactionId\n        ... on ConfirmedObservation {\n          purchase {\n            id\n            merchant\n            occurredAt\n          }\n        }\n        id\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "5de88841b5eefe3637c28e570655acb8";

export default node;

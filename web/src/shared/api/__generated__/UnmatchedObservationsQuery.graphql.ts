/**
 * @generated SignedSource<<96607aae7a1b78fcf8bc831d6bc2a5f6>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
export type UnmatchedObservationState = "CONFIRMED" | "UNMATCHED" | "%future added value";
export type UnmatchedObservationsQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  from?: string | null | undefined;
  householdId: string;
  sourceKind?: SourceKind | null | undefined;
  state?: UnmatchedObservationState | null | undefined;
  to?: string | null | undefined;
};
export type UnmatchedObservationsQuery$data = {
  readonly unmatchedObservations: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly amountYen: any;
        readonly documentId: string;
        readonly merchant: string;
        readonly occurredAt: string | null | undefined;
        readonly orderId: string;
        readonly purchaseId: string | null | undefined;
        readonly sourceKind: SourceKind;
        readonly sourceReference: string;
        readonly state: UnmatchedObservationState;
        readonly transactionId: string;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
    readonly totalCount: number;
  };
};
export type UnmatchedObservationsQuery = {
  response: UnmatchedObservationsQuery$data;
  variables: UnmatchedObservationsQuery$variables;
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
  "name": "householdId"
},
v4 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "sourceKind"
},
v5 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "state"
},
v6 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v7 = [
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
            "concreteType": "UnmatchedObservation",
            "kind": "LinkedField",
            "name": "node",
            "plural": false,
            "selections": [
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
                "name": "purchaseId",
                "storageKey": null
              },
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
                "name": "state",
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
    "name": "UnmatchedObservationsQuery",
    "selections": (v7/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v3/*:: as any*/),
      (v2/*:: as any*/),
      (v6/*:: as any*/),
      (v4/*:: as any*/),
      (v5/*:: as any*/),
      (v0/*:: as any*/),
      (v1/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "UnmatchedObservationsQuery",
    "selections": (v7/*:: as any*/)
  },
  "params": {
    "cacheID": "5270ebc314526eaec14ffb4da1fe7a2f",
    "id": null,
    "metadata": {},
    "name": "UnmatchedObservationsQuery",
    "operationKind": "query",
    "text": "query UnmatchedObservationsQuery(\n  $householdId: ID!\n  $from: String\n  $to: String\n  $sourceKind: SourceKind\n  $state: UnmatchedObservationState\n  $after: String\n  $first: Int\n) {\n  unmatchedObservations(householdId: $householdId, from: $from, to: $to, sourceKind: $sourceKind, state: $state, after: $after, first: $first) {\n    edges {\n      cursor\n      node {\n        documentId\n        sourceReference\n        purchaseId\n        sourceKind\n        state\n        merchant\n        occurredAt\n        amountYen\n        orderId\n        transactionId\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "0b7dd5fbc1cf2e314bb3e0b78d7bf693";

export default node;

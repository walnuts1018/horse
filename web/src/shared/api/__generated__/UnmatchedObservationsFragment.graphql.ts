/**
 * @generated SignedSource<<202032ae46f8cafffb3b1bfc2d994d64>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
import type { FragmentRefs } from "relay-runtime";
export type UnmatchedObservationsFragment$data = {
  readonly unmatchedObservations: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly __typename: string;
        readonly amountYen: any;
        readonly documentId: string;
        readonly merchant: string;
        readonly occurredAt: string | null | undefined;
        readonly orderId: string;
        readonly purchase?: {
          readonly id: string;
          readonly merchant: string;
          readonly occurredAt: string;
        };
        readonly sourceKind: SourceKind;
        readonly sourceReference: string;
        readonly transactionId: string;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
    readonly totalCount: number;
  };
  readonly " $fragmentType": "UnmatchedObservationsFragment";
};
export type UnmatchedObservationsFragment$key = {
  readonly " $data"?: UnmatchedObservationsFragment$data;
  readonly " $fragmentSpreads": FragmentRefs<"UnmatchedObservationsFragment">;
};

import UnmatchedObservationsPaginationQuery_graphql from './UnmatchedObservationsPaginationQuery.graphql';

const node: ReaderFragment = (function(){
var v0 = [
  "unmatchedObservations"
],
v1 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "merchant",
  "storageKey": null
},
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
};
return {
  "argumentDefinitions": [
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
  "kind": "Fragment",
  "metadata": {
    "connection": [
      {
        "count": "first",
        "cursor": "after",
        "direction": "forward",
        "path": (v0/*:: as any*/)
      }
    ],
    "refetch": {
      "connection": {
        "forward": {
          "count": "first",
          "cursor": "after"
        },
        "backward": null,
        "path": (v0/*:: as any*/)
      },
      "fragmentPathInResult": [],
      "operation": UnmatchedObservationsPaginationQuery_graphql
    }
  },
  "name": "UnmatchedObservationsFragment",
  "selections": [
    {
      "alias": "unmatchedObservations",
      "args": [
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
      "name": "__UnmatchedObservationsFragment_unmatchedObservations_connection",
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
                (v1/*:: as any*/),
                (v2/*:: as any*/),
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
                        {
                          "alias": null,
                          "args": null,
                          "kind": "ScalarField",
                          "name": "id",
                          "storageKey": null
                        },
                        (v1/*:: as any*/),
                        (v2/*:: as any*/)
                      ],
                      "storageKey": null
                    }
                  ],
                  "type": "ConfirmedObservation",
                  "abstractKey": null
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
  ],
  "type": "Query",
  "abstractKey": null
};
})();

(node as any).hash = "d73baa0994d486a7a2aceeba30ff9cb1";

export default node;

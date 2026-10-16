/**
 * @generated SignedSource<<49dea92e161c2740cceb4d8772b20fed>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
import type { FragmentRefs } from "relay-runtime";
export type AnalysisUnmatchedObservationsFragment$data = {
  readonly unmatchedObservations: {
    readonly edges: ReadonlyArray<{
      readonly node: {
        readonly __typename: string;
        readonly amountYen?: any;
        readonly id: string;
        readonly merchant?: string;
        readonly occurredAt?: string | null | undefined;
        readonly orderId?: string;
        readonly sourceKind?: SourceKind;
        readonly transactionId?: string;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
    readonly totalCount: number;
  };
  readonly " $fragmentType": "AnalysisUnmatchedObservationsFragment";
};
export type AnalysisUnmatchedObservationsFragment$key = {
  readonly " $data"?: AnalysisUnmatchedObservationsFragment$data;
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisUnmatchedObservationsFragment">;
};

import AnalysisUnmatchedObservationsPaginationQuery_graphql from './AnalysisUnmatchedObservationsPaginationQuery.graphql';

const node: ReaderFragment = (function(){
var v0 = [
  "unmatchedObservations"
];
return {
  "argumentDefinitions": [
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
      "operation": AnalysisUnmatchedObservationsPaginationQuery_graphql
    }
  },
  "name": "AnalysisUnmatchedObservationsFragment",
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
          "name": "to",
          "variableName": "to"
        }
      ],
      "concreteType": "UnmatchedObservationConnection",
      "kind": "LinkedField",
      "name": "__AnalysisUnmatchedObservationsFragment_unmatchedObservations_connection",
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
    }
  ],
  "type": "Query",
  "abstractKey": null
};
})();

(node as any).hash = "c1bec1a5ce3c923cff05f5053e3a1340";

export default node;

/**
 * @generated SignedSource<<89868418ea149fcc17ec9a2b4c9cf353>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
import type { FragmentRefs } from "relay-runtime";
export type AnalysisUndatedUnmatchedObservationsFragment$data = {
  readonly undatedUnmatchedObservations: {
    readonly edges: ReadonlyArray<{
      readonly node: {
        readonly __typename: string;
        readonly amountYen?: any;
        readonly id: string;
        readonly merchant?: string;
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
  readonly " $fragmentType": "AnalysisUndatedUnmatchedObservationsFragment";
};
export type AnalysisUndatedUnmatchedObservationsFragment$key = {
  readonly " $data"?: AnalysisUndatedUnmatchedObservationsFragment$data;
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisUndatedUnmatchedObservationsFragment">;
};

import AnalysisUndatedUnmatchedObservationsPaginationQuery_graphql from './AnalysisUndatedUnmatchedObservationsPaginationQuery.graphql';

const node: ReaderFragment = (function(){
var v0 = [
  "undatedUnmatchedObservations"
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
      "name": "householdId"
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
      "operation": AnalysisUndatedUnmatchedObservationsPaginationQuery_graphql
    }
  },
  "name": "AnalysisUndatedUnmatchedObservationsFragment",
  "selections": [
    {
      "alias": "undatedUnmatchedObservations",
      "args": [
        {
          "kind": "Variable",
          "name": "householdId",
          "variableName": "householdId"
        }
      ],
      "concreteType": "UnmatchedObservationConnection",
      "kind": "LinkedField",
      "name": "__AnalysisUndatedUnmatchedObservationsFragment_undatedUnmatchedObservations_connection",
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
    }
  ],
  "type": "Query",
  "abstractKey": null
};
})();

(node as any).hash = "5ef1b23b0130155a37c7006826968b25";

export default node;

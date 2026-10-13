/**
 * @generated SignedSource<<e90c1dfa016c0db66de825c13077349e>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type HouseholdLedgerFragment$data = {
  readonly ledger: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly __typename: string;
        readonly amountYen: any;
        readonly createdAt: string;
        readonly currency: string;
        readonly description: string;
        readonly fromMember?: {
          readonly displayName: string;
          readonly id: string;
        };
        readonly id: string;
        readonly loan?: {
          readonly id: string;
        };
        readonly member?: {
          readonly displayName: string;
          readonly id: string;
        };
        readonly occurredAt: string;
        readonly pool?: {
          readonly id: string;
          readonly name: string;
        };
        readonly purchase?: {
          readonly id: string;
        };
        readonly toMember?: {
          readonly displayName: string;
          readonly id: string;
        };
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
    readonly totalCount: number;
  };
  readonly " $fragmentType": "HouseholdLedgerFragment";
};
export type HouseholdLedgerFragment$key = {
  readonly " $data"?: HouseholdLedgerFragment$data;
  readonly " $fragmentSpreads": FragmentRefs<"HouseholdLedgerFragment">;
};

import HouseholdLedgerPaginationQuery_graphql from './HouseholdLedgerPaginationQuery.graphql';

const node: ReaderFragment = (function(){
var v0 = [
  "ledger"
],
v1 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v2 = [
  (v1/*:: as any*/)
],
v3 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "purchase",
    "plural": false,
    "selections": (v2/*:: as any*/),
    "storageKey": null
  }
],
v4 = [
  (v1/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "displayName",
    "storageKey": null
  }
],
v5 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "LoanBalance",
    "kind": "LinkedField",
    "name": "loan",
    "plural": false,
    "selections": (v2/*:: as any*/),
    "storageKey": null
  }
];
return {
  "argumentDefinitions": [
    {
      "defaultValue": null,
      "kind": "LocalArgument",
      "name": "after"
    },
    {
      "defaultValue": 100,
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
      "operation": HouseholdLedgerPaginationQuery_graphql
    }
  },
  "name": "HouseholdLedgerFragment",
  "selections": [
    {
      "alias": "ledger",
      "args": [
        {
          "kind": "Variable",
          "name": "householdId",
          "variableName": "householdId"
        }
      ],
      "concreteType": "LedgerConnection",
      "kind": "LinkedField",
      "name": "__HouseholdLedgerFragment_ledger_connection",
      "plural": false,
      "selections": [
        {
          "alias": null,
          "args": null,
          "concreteType": "LedgerEdge",
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
                (v1/*:: as any*/),
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
                  "name": "description",
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
                  "name": "currency",
                  "storageKey": null
                },
                {
                  "alias": null,
                  "args": null,
                  "kind": "ScalarField",
                  "name": "createdAt",
                  "storageKey": null
                },
                {
                  "kind": "InlineFragment",
                  "selections": (v3/*:: as any*/),
                  "type": "PurchaseLedgerEntry",
                  "abstractKey": null
                },
                {
                  "kind": "InlineFragment",
                  "selections": [
                    {
                      "alias": null,
                      "args": null,
                      "concreteType": "Member",
                      "kind": "LinkedField",
                      "name": "member",
                      "plural": false,
                      "selections": (v4/*:: as any*/),
                      "storageKey": null
                    },
                    {
                      "alias": null,
                      "args": null,
                      "concreteType": null,
                      "kind": "LinkedField",
                      "name": "pool",
                      "plural": false,
                      "selections": [
                        (v1/*:: as any*/),
                        {
                          "alias": null,
                          "args": null,
                          "kind": "ScalarField",
                          "name": "name",
                          "storageKey": null
                        }
                      ],
                      "storageKey": null
                    }
                  ],
                  "type": "FundingLedgerEntry",
                  "abstractKey": null
                },
                {
                  "kind": "InlineFragment",
                  "selections": [
                    {
                      "alias": null,
                      "args": null,
                      "concreteType": "Member",
                      "kind": "LinkedField",
                      "name": "fromMember",
                      "plural": false,
                      "selections": (v4/*:: as any*/),
                      "storageKey": null
                    },
                    {
                      "alias": null,
                      "args": null,
                      "concreteType": "Member",
                      "kind": "LinkedField",
                      "name": "toMember",
                      "plural": false,
                      "selections": (v4/*:: as any*/),
                      "storageKey": null
                    }
                  ],
                  "type": "TransferLedgerEntry",
                  "abstractKey": "__isTransferLedgerEntry"
                },
                {
                  "kind": "InlineFragment",
                  "selections": (v5/*:: as any*/),
                  "type": "LoanTransferLedgerEntry",
                  "abstractKey": null
                },
                {
                  "kind": "InlineFragment",
                  "selections": (v5/*:: as any*/),
                  "type": "RepaymentTransferLedgerEntry",
                  "abstractKey": null
                },
                {
                  "kind": "InlineFragment",
                  "selections": (v3/*:: as any*/),
                  "type": "CorrectionLedgerEntry",
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

(node as any).hash = "4c4a36e5897949e520f8159a3261fb85";

export default node;

/**
 * @generated SignedSource<<f991a5351262ea787a553712463fb472>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type HouseholdLedgerPaginationQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type HouseholdLedgerPaginationQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"HouseholdLedgerFragment">;
};
export type HouseholdLedgerPaginationQuery = {
  response: HouseholdLedgerPaginationQuery$data;
  variables: HouseholdLedgerPaginationQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
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
],
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v4 = [
  (v3/*:: as any*/)
],
v5 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "purchase",
    "plural": false,
    "selections": (v4/*:: as any*/),
    "storageKey": null
  }
],
v6 = [
  (v3/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "displayName",
    "storageKey": null
  }
],
v7 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "LoanBalance",
    "kind": "LinkedField",
    "name": "loan",
    "plural": false,
    "selections": (v4/*:: as any*/),
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "HouseholdLedgerPaginationQuery",
    "selections": [
      {
        "args": (v1/*:: as any*/),
        "kind": "FragmentSpread",
        "name": "HouseholdLedgerFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "HouseholdLedgerPaginationQuery",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": "LedgerConnection",
        "kind": "LinkedField",
        "name": "ledger",
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
                  (v2/*:: as any*/),
                  {
                    "kind": "TypeDiscriminator",
                    "abstractKey": "__isLedgerEntry"
                  },
                  (v3/*:: as any*/),
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
                    "selections": (v5/*:: as any*/),
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
                        "selections": (v6/*:: as any*/),
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
                          (v2/*:: as any*/),
                          (v3/*:: as any*/),
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
                        "selections": (v6/*:: as any*/),
                        "storageKey": null
                      },
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "Member",
                        "kind": "LinkedField",
                        "name": "toMember",
                        "plural": false,
                        "selections": (v6/*:: as any*/),
                        "storageKey": null
                      }
                    ],
                    "type": "TransferLedgerEntry",
                    "abstractKey": "__isTransferLedgerEntry"
                  },
                  {
                    "kind": "InlineFragment",
                    "selections": (v7/*:: as any*/),
                    "type": "LoanTransferLedgerEntry",
                    "abstractKey": null
                  },
                  {
                    "kind": "InlineFragment",
                    "selections": (v7/*:: as any*/),
                    "type": "RepaymentTransferLedgerEntry",
                    "abstractKey": null
                  },
                  {
                    "kind": "InlineFragment",
                    "selections": (v5/*:: as any*/),
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
      },
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "filters": [
          "householdId"
        ],
        "handle": "connection",
        "key": "HouseholdLedgerFragment_ledger",
        "kind": "LinkedHandle",
        "name": "ledger"
      }
    ]
  },
  "params": {
    "cacheID": "f916ec9290a48459283e105e4c3156b4",
    "id": null,
    "metadata": {},
    "name": "HouseholdLedgerPaginationQuery",
    "operationKind": "query",
    "text": "query HouseholdLedgerPaginationQuery(\n  $after: String\n  $first: Int = 100\n  $householdId: ID!\n) {\n  ...HouseholdLedgerFragment_1x5y0e\n}\n\nfragment HouseholdLedgerFragment_1x5y0e on Query {\n  ledger(householdId: $householdId, first: $first, after: $after) {\n    edges {\n      cursor\n      node {\n        __typename\n        __isLedgerEntry: __typename\n        id\n        occurredAt\n        description\n        amountYen\n        currency\n        createdAt\n        ... on PurchaseLedgerEntry {\n          purchase {\n            id\n          }\n        }\n        ... on FundingLedgerEntry {\n          member {\n            id\n            displayName\n          }\n          pool {\n            __typename\n            id\n            name\n          }\n        }\n        ... on TransferLedgerEntry {\n          __isTransferLedgerEntry: __typename\n          fromMember {\n            id\n            displayName\n          }\n          toMember {\n            id\n            displayName\n          }\n        }\n        ... on LoanTransferLedgerEntry {\n          loan {\n            id\n          }\n        }\n        ... on RepaymentTransferLedgerEntry {\n          loan {\n            id\n          }\n        }\n        ... on CorrectionLedgerEntry {\n          purchase {\n            id\n          }\n        }\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "4c4a36e5897949e520f8159a3261fb85";

export default node;

/**
 * @generated SignedSource<<3812e5f77c12290e7bc4093c6b9e9ddd>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type LedgerQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type LedgerQuery$data = {
  readonly ledger: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly " $fragmentSpreads": FragmentRefs<"LedgerEntryFields">;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
    readonly totalCount: number;
  };
};
export type LedgerQuery = {
  response: LedgerQuery$data;
  variables: LedgerQuery$variables;
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
  "name": "householdId"
},
v3 = [
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
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "cursor",
  "storageKey": null
},
v5 = {
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
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "totalCount",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v8 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "purchase",
    "plural": false,
    "selections": [
      (v7/*:: as any*/)
    ],
    "storageKey": null
  }
],
v9 = [
  (v7/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "displayName",
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "LedgerQuery",
    "selections": [
      {
        "alias": null,
        "args": (v3/*:: as any*/),
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
              (v4/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": null,
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
                "selections": [
                  {
                    "args": null,
                    "kind": "FragmentSpread",
                    "name": "LedgerEntryFields"
                  }
                ],
                "storageKey": null
              }
            ],
            "storageKey": null
          },
          (v5/*:: as any*/),
          (v6/*:: as any*/)
        ],
        "storageKey": null
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v0/*:: as any*/),
      (v1/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "LedgerQuery",
    "selections": [
      {
        "alias": null,
        "args": (v3/*:: as any*/),
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
              (v4/*:: as any*/),
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
                    "kind": "TypeDiscriminator",
                    "abstractKey": "__isLedgerEntry"
                  },
                  (v7/*:: as any*/),
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
                    "selections": (v8/*:: as any*/),
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
                        "selections": (v9/*:: as any*/),
                        "storageKey": null
                      },
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "Pool",
                        "kind": "LinkedField",
                        "name": "pool",
                        "plural": false,
                        "selections": [
                          (v7/*:: as any*/),
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
                        "selections": (v9/*:: as any*/),
                        "storageKey": null
                      },
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "Member",
                        "kind": "LinkedField",
                        "name": "toMember",
                        "plural": false,
                        "selections": (v9/*:: as any*/),
                        "storageKey": null
                      },
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "transferKind",
                        "storageKey": null
                      },
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "LoanBalance",
                        "kind": "LinkedField",
                        "name": "loan",
                        "plural": false,
                        "selections": [
                          {
                            "alias": null,
                            "args": null,
                            "kind": "ScalarField",
                            "name": "loanId",
                            "storageKey": null
                          }
                        ],
                        "storageKey": null
                      }
                    ],
                    "type": "TransferLedgerEntry",
                    "abstractKey": null
                  },
                  {
                    "kind": "InlineFragment",
                    "selections": (v8/*:: as any*/),
                    "type": "CorrectionLedgerEntry",
                    "abstractKey": null
                  }
                ],
                "storageKey": null
              }
            ],
            "storageKey": null
          },
          (v5/*:: as any*/),
          (v6/*:: as any*/)
        ],
        "storageKey": null
      }
    ]
  },
  "params": {
    "cacheID": "fbb4baf12f9ed04a2e2e8da7c6d1c95c",
    "id": null,
    "metadata": {},
    "name": "LedgerQuery",
    "operationKind": "query",
    "text": "query LedgerQuery(\n  $householdId: ID!\n  $after: String\n  $first: Int\n) {\n  ledger(householdId: $householdId, after: $after, first: $first) {\n    edges {\n      cursor\n      node {\n        __typename\n        ...LedgerEntryFields\n        id\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n\nfragment LedgerEntryFields on LedgerEntry {\n  __isLedgerEntry: __typename\n  __typename\n  id\n  occurredAt\n  description\n  amountYen\n  currency\n  createdAt\n  ... on PurchaseLedgerEntry {\n    purchase {\n      id\n    }\n  }\n  ... on FundingLedgerEntry {\n    member {\n      id\n      displayName\n    }\n    pool {\n      id\n      name\n    }\n  }\n  ... on TransferLedgerEntry {\n    fromMember {\n      id\n      displayName\n    }\n    toMember {\n      id\n      displayName\n    }\n    transferKind\n    loan {\n      loanId\n    }\n  }\n  ... on CorrectionLedgerEntry {\n    purchase {\n      id\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "e3e5e320e61803f10014d69d615db9f6";

export default node;

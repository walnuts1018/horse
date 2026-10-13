/**
 * @generated SignedSource<<5b0f599d529a1e8bb12eea7861de13c0>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountBalanceAnalysisQuery$variables = {
  accountId: string;
  after?: string | null | undefined;
  first?: number | null | undefined;
  from: string;
  householdId: string;
  to: string;
};
export type AccountBalanceAnalysisQuery$data = {
  readonly accountBalanceAnalysis: {
    readonly accountId: string;
    readonly changes: {
      readonly edges: ReadonlyArray<{
        readonly cursor: string;
        readonly node: {
          readonly balanceYen: any;
          readonly changeYen: any;
          readonly description: string;
          readonly occurredAt: string;
          readonly source: {
            readonly __typename: "AccountIncome";
            readonly id: string;
          } | {
            readonly __typename: "AccountMovement";
            readonly id: string;
          } | {
            readonly __typename: "AccountOpeningBalance";
            readonly account: {
              readonly id: string;
              readonly name: string;
            };
            readonly id: string;
          } | {
            readonly __typename: "AdvanceTransferLedgerEntry";
            readonly id: string;
          } | {
            readonly __typename: "CorrectionLedgerEntry";
            readonly id: string;
          } | {
            readonly __typename: "FundingLedgerEntry";
            readonly id: string;
          } | {
            readonly __typename: "GiftTransferLedgerEntry";
            readonly id: string;
          } | {
            readonly __typename: "LoanTransferLedgerEntry";
            readonly id: string;
          } | {
            readonly __typename: "Purchase";
            readonly id: string;
          } | {
            readonly __typename: "PurchaseItemCorrection";
            readonly id: string;
          } | {
            readonly __typename: "PurchaseLedgerEntry";
            readonly id: string;
          } | {
            readonly __typename: "PurchaseWideCorrection";
            readonly id: string;
          } | {
            readonly __typename: "RepaymentTransferLedgerEntry";
            readonly id: string;
          } | {
            // This will never be '%other', but we need some
            // value in case none of the concrete values match.
            readonly __typename: "%other";
          };
        };
      }>;
      readonly pageInfo: {
        readonly endCursor: string | null | undefined;
        readonly hasNextPage: boolean;
      };
      readonly totalCount: number;
    };
    readonly closingBalanceYen: any;
    readonly currency: string;
    readonly from: string;
    readonly openingBalanceYen: any;
    readonly to: string;
  };
};
export type AccountBalanceAnalysisQuery = {
  response: AccountBalanceAnalysisQuery$data;
  variables: AccountBalanceAnalysisQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "accountId"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "after"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "from"
},
v4 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v5 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v6 = [
  {
    "kind": "Variable",
    "name": "accountId",
    "variableName": "accountId"
  },
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
    "name": "to",
    "variableName": "to"
  }
],
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "accountId",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "currency",
  "storageKey": null
},
v9 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "from",
  "storageKey": null
},
v10 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "to",
  "storageKey": null
},
v11 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "openingBalanceYen",
  "storageKey": null
},
v12 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "closingBalanceYen",
  "storageKey": null
},
v13 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "cursor",
  "storageKey": null
},
v14 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
},
v15 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v16 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v17 = {
  "kind": "InlineFragment",
  "selections": [
    (v16/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "concreteType": "Account",
      "kind": "LinkedField",
      "name": "account",
      "plural": false,
      "selections": [
        (v16/*:: as any*/),
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
  "type": "AccountOpeningBalance",
  "abstractKey": null
},
v18 = [
  (v16/*:: as any*/)
],
v19 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "Purchase",
  "abstractKey": null
},
v20 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "PurchaseWideCorrection",
  "abstractKey": null
},
v21 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "PurchaseItemCorrection",
  "abstractKey": null
},
v22 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "CorrectionLedgerEntry",
  "abstractKey": null
},
v23 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "FundingLedgerEntry",
  "abstractKey": null
},
v24 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "PurchaseLedgerEntry",
  "abstractKey": null
},
v25 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "AdvanceTransferLedgerEntry",
  "abstractKey": null
},
v26 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "LoanTransferLedgerEntry",
  "abstractKey": null
},
v27 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "RepaymentTransferLedgerEntry",
  "abstractKey": null
},
v28 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "GiftTransferLedgerEntry",
  "abstractKey": null
},
v29 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "AccountMovement",
  "abstractKey": null
},
v30 = {
  "kind": "InlineFragment",
  "selections": (v18/*:: as any*/),
  "type": "AccountIncome",
  "abstractKey": null
},
v31 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "description",
  "storageKey": null
},
v32 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "changeYen",
  "storageKey": null
},
v33 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "balanceYen",
  "storageKey": null
},
v34 = {
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
v35 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "totalCount",
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
    "name": "AccountBalanceAnalysisQuery",
    "selections": [
      {
        "alias": null,
        "args": (v6/*:: as any*/),
        "concreteType": "AccountBalanceAnalysis",
        "kind": "LinkedField",
        "name": "accountBalanceAnalysis",
        "plural": false,
        "selections": [
          (v7/*:: as any*/),
          (v8/*:: as any*/),
          (v9/*:: as any*/),
          (v10/*:: as any*/),
          (v11/*:: as any*/),
          (v12/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "concreteType": "AccountBalanceChangeConnection",
            "kind": "LinkedField",
            "name": "changes",
            "plural": false,
            "selections": [
              {
                "alias": null,
                "args": null,
                "concreteType": "AccountBalanceChangeEdge",
                "kind": "LinkedField",
                "name": "edges",
                "plural": true,
                "selections": [
                  (v13/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "AccountBalanceChange",
                    "kind": "LinkedField",
                    "name": "node",
                    "plural": false,
                    "selections": [
                      (v14/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": null,
                        "kind": "LinkedField",
                        "name": "source",
                        "plural": false,
                        "selections": [
                          (v15/*:: as any*/),
                          (v17/*:: as any*/),
                          (v19/*:: as any*/),
                          (v20/*:: as any*/),
                          (v21/*:: as any*/),
                          (v22/*:: as any*/),
                          (v23/*:: as any*/),
                          (v24/*:: as any*/),
                          (v25/*:: as any*/),
                          (v26/*:: as any*/),
                          (v27/*:: as any*/),
                          (v28/*:: as any*/),
                          (v29/*:: as any*/),
                          (v30/*:: as any*/)
                        ],
                        "storageKey": null
                      },
                      (v31/*:: as any*/),
                      (v32/*:: as any*/),
                      (v33/*:: as any*/)
                    ],
                    "storageKey": null
                  }
                ],
                "storageKey": null
              },
              (v34/*:: as any*/),
              (v35/*:: as any*/)
            ],
            "storageKey": null
          }
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
      (v4/*:: as any*/),
      (v0/*:: as any*/),
      (v3/*:: as any*/),
      (v5/*:: as any*/),
      (v2/*:: as any*/),
      (v1/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "AccountBalanceAnalysisQuery",
    "selections": [
      {
        "alias": null,
        "args": (v6/*:: as any*/),
        "concreteType": "AccountBalanceAnalysis",
        "kind": "LinkedField",
        "name": "accountBalanceAnalysis",
        "plural": false,
        "selections": [
          (v7/*:: as any*/),
          (v8/*:: as any*/),
          (v9/*:: as any*/),
          (v10/*:: as any*/),
          (v11/*:: as any*/),
          (v12/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "concreteType": "AccountBalanceChangeConnection",
            "kind": "LinkedField",
            "name": "changes",
            "plural": false,
            "selections": [
              {
                "alias": null,
                "args": null,
                "concreteType": "AccountBalanceChangeEdge",
                "kind": "LinkedField",
                "name": "edges",
                "plural": true,
                "selections": [
                  (v13/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "AccountBalanceChange",
                    "kind": "LinkedField",
                    "name": "node",
                    "plural": false,
                    "selections": [
                      (v14/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": null,
                        "kind": "LinkedField",
                        "name": "source",
                        "plural": false,
                        "selections": [
                          (v15/*:: as any*/),
                          (v17/*:: as any*/),
                          (v19/*:: as any*/),
                          (v20/*:: as any*/),
                          (v21/*:: as any*/),
                          (v22/*:: as any*/),
                          (v23/*:: as any*/),
                          (v24/*:: as any*/),
                          (v25/*:: as any*/),
                          (v26/*:: as any*/),
                          (v27/*:: as any*/),
                          (v28/*:: as any*/),
                          (v29/*:: as any*/),
                          (v30/*:: as any*/),
                          {
                            "kind": "InlineFragment",
                            "selections": (v18/*:: as any*/),
                            "type": "Node",
                            "abstractKey": "__isNode"
                          }
                        ],
                        "storageKey": null
                      },
                      (v31/*:: as any*/),
                      (v32/*:: as any*/),
                      (v33/*:: as any*/)
                    ],
                    "storageKey": null
                  }
                ],
                "storageKey": null
              },
              (v34/*:: as any*/),
              (v35/*:: as any*/)
            ],
            "storageKey": null
          }
        ],
        "storageKey": null
      }
    ]
  },
  "params": {
    "cacheID": "e0ea05b28f0e484112664508bc35fe61",
    "id": null,
    "metadata": {},
    "name": "AccountBalanceAnalysisQuery",
    "operationKind": "query",
    "text": "query AccountBalanceAnalysisQuery(\n  $householdId: ID!\n  $accountId: ID!\n  $from: YearMonth!\n  $to: YearMonth!\n  $first: Int\n  $after: String\n) {\n  accountBalanceAnalysis(householdId: $householdId, accountId: $accountId, from: $from, to: $to, first: $first, after: $after) {\n    accountId\n    currency\n    from\n    to\n    openingBalanceYen\n    closingBalanceYen\n    changes {\n      edges {\n        cursor\n        node {\n          occurredAt\n          source {\n            __typename\n            ... on AccountOpeningBalance {\n              id\n              account {\n                id\n                name\n              }\n            }\n            ... on Purchase {\n              id\n            }\n            ... on PurchaseWideCorrection {\n              id\n            }\n            ... on PurchaseItemCorrection {\n              id\n            }\n            ... on CorrectionLedgerEntry {\n              id\n            }\n            ... on FundingLedgerEntry {\n              id\n            }\n            ... on PurchaseLedgerEntry {\n              id\n            }\n            ... on AdvanceTransferLedgerEntry {\n              id\n            }\n            ... on LoanTransferLedgerEntry {\n              id\n            }\n            ... on RepaymentTransferLedgerEntry {\n              id\n            }\n            ... on GiftTransferLedgerEntry {\n              id\n            }\n            ... on AccountMovement {\n              id\n            }\n            ... on AccountIncome {\n              id\n            }\n            ... on Node {\n              __isNode: __typename\n              id\n            }\n          }\n          description\n          changeYen\n          balanceYen\n        }\n      }\n      pageInfo {\n        endCursor\n        hasNextPage\n      }\n      totalCount\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "64363c3e7fadbeafeac08f02cd0a1814";

export default node;

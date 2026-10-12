/**
 * @generated SignedSource<<bb78bac21140906ae624d3a56ea10387>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type RecordFundingInput = {
  amountYen: any;
  destinationAccountId?: string | null | undefined;
  householdId: string;
  idempotencyKey: string;
  memberId: string;
  occurredAt: string;
  poolId: string;
  sourceAccountId?: string | null | undefined;
};
export type RecordFundingMutation$variables = {
  input: RecordFundingInput;
};
export type RecordFundingMutation$data = {
  readonly recordFunding: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly " $fragmentSpreads": FragmentRefs<"LedgerEntryFields">;
    };
  };
};
export type RecordFundingMutation = {
  response: RecordFundingMutation$data;
  variables: RecordFundingMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "input"
  }
],
v1 = [
  {
    "kind": "Variable",
    "name": "input",
    "variableName": "input"
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
  "kind": "InlineFragment",
  "selections": [
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "code",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "message",
      "storageKey": null
    }
  ],
  "type": "MutationError",
  "abstractKey": "__isMutationError"
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v5 = [
  (v4/*:: as any*/)
],
v6 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "purchase",
    "plural": false,
    "selections": (v5/*:: as any*/),
    "storageKey": null
  }
],
v7 = [
  (v4/*:: as any*/),
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
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "RecordFundingMutation",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "recordFunding",
        "plural": false,
        "selections": [
          (v2/*:: as any*/),
          {
            "kind": "InlineFragment",
            "selections": [
              {
                "alias": null,
                "args": null,
                "concreteType": null,
                "kind": "LinkedField",
                "name": "result",
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
            "type": "RecordFundingSuccess",
            "abstractKey": null
          },
          (v3/*:: as any*/)
        ],
        "storageKey": null
      }
    ],
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecordFundingMutation",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "recordFunding",
        "plural": false,
        "selections": [
          (v2/*:: as any*/),
          {
            "kind": "InlineFragment",
            "selections": [
              {
                "alias": null,
                "args": null,
                "concreteType": null,
                "kind": "LinkedField",
                "name": "result",
                "plural": false,
                "selections": [
                  (v2/*:: as any*/),
                  {
                    "kind": "TypeDiscriminator",
                    "abstractKey": "__isLedgerEntry"
                  },
                  (v4/*:: as any*/),
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
                    "selections": (v6/*:: as any*/),
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
                        "selections": (v7/*:: as any*/),
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
                          (v4/*:: as any*/),
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
                        "selections": (v7/*:: as any*/),
                        "storageKey": null
                      },
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "Member",
                        "kind": "LinkedField",
                        "name": "toMember",
                        "plural": false,
                        "selections": (v7/*:: as any*/),
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
                        "selections": (v5/*:: as any*/),
                        "storageKey": null
                      }
                    ],
                    "type": "TransferLedgerEntry",
                    "abstractKey": null
                  },
                  {
                    "kind": "InlineFragment",
                    "selections": (v6/*:: as any*/),
                    "type": "CorrectionLedgerEntry",
                    "abstractKey": null
                  }
                ],
                "storageKey": null
              }
            ],
            "type": "RecordFundingSuccess",
            "abstractKey": null
          },
          (v3/*:: as any*/)
        ],
        "storageKey": null
      }
    ]
  },
  "params": {
    "cacheID": "e622bf317a40658cb08565f74e304a9f",
    "id": null,
    "metadata": {},
    "name": "RecordFundingMutation",
    "operationKind": "mutation",
    "text": "mutation RecordFundingMutation(\n  $input: RecordFundingInput!\n) {\n  recordFunding(input: $input) {\n    __typename\n    ... on RecordFundingSuccess {\n      result {\n        __typename\n        ...LedgerEntryFields\n        id\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n\nfragment LedgerEntryFields on LedgerEntry {\n  __isLedgerEntry: __typename\n  __typename\n  id\n  occurredAt\n  description\n  amountYen\n  currency\n  createdAt\n  ... on PurchaseLedgerEntry {\n    purchase {\n      id\n    }\n  }\n  ... on FundingLedgerEntry {\n    member {\n      id\n      displayName\n    }\n    pool {\n      id\n      name\n    }\n  }\n  ... on TransferLedgerEntry {\n    fromMember {\n      id\n      displayName\n    }\n    toMember {\n      id\n      displayName\n    }\n    transferKind\n    loan {\n      id\n    }\n  }\n  ... on CorrectionLedgerEntry {\n    purchase {\n      id\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "fc60d67bafc81ccd03b7097658d7fe8d";

export default node;

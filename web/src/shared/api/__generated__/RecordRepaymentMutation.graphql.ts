/**
 * @generated SignedSource<<276da8c79502d084dce7a3acaf93f2e9>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type RecordRepaymentInput = {
  amountYen: any;
  description: string;
  destinationAccountId?: string | null | undefined;
  fromMemberId: string;
  householdId: string;
  idempotencyKey: string;
  loanId: string;
  occurredAt: string;
  paymentAccountId?: string | null | undefined;
  toMemberId: string;
};
export type RecordRepaymentMutation$variables = {
  input: RecordRepaymentInput;
};
export type RecordRepaymentMutation$data = {
  readonly recordRepayment: {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "RecordRepaymentSuccess";
    readonly result: {
      readonly " $fragmentSpreads": FragmentRefs<"LedgerEntryFields">;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type RecordRepaymentMutation = {
  response: RecordRepaymentMutation$data;
  variables: RecordRepaymentMutation$variables;
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
  "abstractKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v5 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "purchase",
    "plural": false,
    "selections": [
      (v4/*:: as any*/)
    ],
    "storageKey": null
  }
],
v6 = [
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
    "name": "RecordRepaymentMutation",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "recordRepayment",
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
            "type": "RecordRepaymentSuccess",
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
    "name": "RecordRepaymentMutation",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "recordRepayment",
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
                    "selections": (v5/*:: as any*/),
                    "type": "CorrectionLedgerEntry",
                    "abstractKey": null
                  }
                ],
                "storageKey": null
              }
            ],
            "type": "RecordRepaymentSuccess",
            "abstractKey": null
          },
          (v3/*:: as any*/)
        ],
        "storageKey": null
      }
    ]
  },
  "params": {
    "cacheID": "d5d1fb4d2b006b89613cc74a834963aa",
    "id": null,
    "metadata": {},
    "name": "RecordRepaymentMutation",
    "operationKind": "mutation",
    "text": "mutation RecordRepaymentMutation(\n  $input: RecordRepaymentInput!\n) {\n  recordRepayment(input: $input) {\n    __typename\n    ... on RecordRepaymentSuccess {\n      result {\n        __typename\n        ...LedgerEntryFields\n        id\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n\nfragment LedgerEntryFields on LedgerEntry {\n  __isLedgerEntry: __typename\n  __typename\n  id\n  occurredAt\n  description\n  amountYen\n  currency\n  createdAt\n  ... on PurchaseLedgerEntry {\n    purchase {\n      id\n    }\n  }\n  ... on FundingLedgerEntry {\n    member {\n      id\n      displayName\n    }\n    pool {\n      id\n      name\n    }\n  }\n  ... on TransferLedgerEntry {\n    fromMember {\n      id\n      displayName\n    }\n    toMember {\n      id\n      displayName\n    }\n    transferKind\n    loan {\n      loanId\n    }\n  }\n  ... on CorrectionLedgerEntry {\n    purchase {\n      id\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "1a53ae697b36d1c36d3fb0b1334af0a3";

export default node;

/**
 * @generated SignedSource<<1d3525d7e91e77bbd983e248b5d4757d>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type TransferInputKind = "ADVANCE" | "GIFT" | "LOAN" | "%future added value";
export type RecordTransferInput = {
  amountYen: any;
  description: string;
  destinationAccountId?: string | null | undefined;
  fromMemberId: string;
  householdId: string;
  idempotencyKey: string;
  kind: TransferInputKind;
  occurredAt: string;
  paymentAccountId?: string | null | undefined;
  toMemberId: string;
};
export type RecordTransferMutation$variables = {
  input: RecordTransferInput;
};
export type RecordTransferMutation$data = {
  readonly recordTransfer: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly " $fragmentSpreads": FragmentRefs<"LedgerEntryFields">;
    };
  };
};
export type RecordTransferMutation = {
  response: RecordTransferMutation$data;
  variables: RecordTransferMutation$variables;
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
    "name": "RecordTransferMutation",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "recordTransfer",
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
            "type": "RecordTransferSuccess",
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
    "name": "RecordTransferMutation",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "recordTransfer",
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
            "type": "RecordTransferSuccess",
            "abstractKey": null
          },
          (v3/*:: as any*/)
        ],
        "storageKey": null
      }
    ]
  },
  "params": {
    "cacheID": "59177890fa38d08337e13b183a7ef2da",
    "id": null,
    "metadata": {},
    "name": "RecordTransferMutation",
    "operationKind": "mutation",
    "text": "mutation RecordTransferMutation(\n  $input: RecordTransferInput!\n) {\n  recordTransfer(input: $input) {\n    __typename\n    ... on RecordTransferSuccess {\n      result {\n        __typename\n        ...LedgerEntryFields\n        id\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n\nfragment LedgerEntryFields on LedgerEntry {\n  __isLedgerEntry: __typename\n  __typename\n  id\n  occurredAt\n  description\n  amountYen\n  currency\n  createdAt\n  ... on PurchaseLedgerEntry {\n    purchase {\n      id\n    }\n  }\n  ... on FundingLedgerEntry {\n    member {\n      id\n      displayName\n    }\n    pool {\n      id\n      name\n    }\n  }\n  ... on TransferLedgerEntry {\n    fromMember {\n      id\n      displayName\n    }\n    toMember {\n      id\n      displayName\n    }\n    transferKind\n    loan {\n      id\n    }\n  }\n  ... on CorrectionLedgerEntry {\n    purchase {\n      id\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "9fa3415e5e8c933c17fbaec2cda5caae";

export default node;

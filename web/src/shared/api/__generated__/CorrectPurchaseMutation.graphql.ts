/**
 * @generated SignedSource<<3aa5f2308bb5a9305063d170de467769>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type CorrectPurchaseInput = {
  allocations?: ReadonlyArray<AllocationInput> | null | undefined;
  amountDeltaYen: any;
  householdId: string;
  idempotencyKey: string;
  itemId?: string | null | undefined;
  paymentAllocations?: ReadonlyArray<PurchasePaymentInput> | null | undefined;
  purchaseId: string;
  reason: string;
};
export type AllocationInput = {
  poolId: string;
  weight: any;
};
export type PurchasePaymentInput = {
  amountYen: any;
  paymentAccountId: string;
};
export type CorrectPurchaseMutation$variables = {
  input: CorrectPurchaseInput;
};
export type CorrectPurchaseMutation$data = {
  readonly correctPurchase: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
      readonly revision: number;
    };
  };
};
export type CorrectPurchaseMutation = {
  response: CorrectPurchaseMutation$data;
  variables: CorrectPurchaseMutation$variables;
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
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "input",
        "variableName": "input"
      }
    ],
    "concreteType": null,
    "kind": "LinkedField",
    "name": "correctPurchase",
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
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "Purchase",
            "kind": "LinkedField",
            "name": "result",
            "plural": false,
            "selections": [
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "id",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "revision",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "CorrectPurchaseSuccess",
        "abstractKey": null
      },
      {
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
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "CorrectPurchaseMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CorrectPurchaseMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "3681b902109936b4493f17dec9b049b3",
    "id": null,
    "metadata": {},
    "name": "CorrectPurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation CorrectPurchaseMutation(\n  $input: CorrectPurchaseInput!\n) {\n  correctPurchase(input: $input) {\n    __typename\n    ... on CorrectPurchaseSuccess {\n      result {\n        id\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "356713b4c0e954e2e1dfa972cb0ef019";

export default node;

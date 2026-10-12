/**
 * @generated SignedSource<<209e7aa2af46fee47cc438bd39c30246>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type UpdatePurchaseInput = {
  baseRevision: number;
  changedFields: ReadonlyArray<string>;
  patch: PurchasePatch;
  purchaseId: string;
};
export type PurchasePatch = {
  merchant?: string | null | undefined;
  occurredAt?: string | null | undefined;
  paymentAllocations?: ReadonlyArray<PurchasePaymentInput> | null | undefined;
};
export type PurchasePaymentInput = {
  amountYen: any;
  paymentAccountId: string;
};
export type UpdatePurchaseMutation$variables = {
  input: UpdatePurchaseInput;
};
export type UpdatePurchaseMutation$data = {
  readonly updatePurchase: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
      readonly merchant: string;
      readonly occurredAt: string;
      readonly revision: number;
    };
  };
};
export type UpdatePurchaseMutation = {
  response: UpdatePurchaseMutation$data;
  variables: UpdatePurchaseMutation$variables;
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
    "name": "updatePurchase",
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
                "name": "merchant",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "UpdatePurchaseSuccess",
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
    "name": "UpdatePurchaseMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "UpdatePurchaseMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "dd8491f5be8c2f4d79d15e767545dc08",
    "id": null,
    "metadata": {},
    "name": "UpdatePurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation UpdatePurchaseMutation(\n  $input: UpdatePurchaseInput!\n) {\n  updatePurchase(input: $input) {\n    __typename\n    ... on UpdatePurchaseSuccess {\n      result {\n        id\n        revision\n        occurredAt\n        merchant\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "a0e3dae3ed399af49b16389b1f3ca54a";

export default node;

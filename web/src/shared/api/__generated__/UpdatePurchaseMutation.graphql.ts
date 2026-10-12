/**
 * @generated SignedSource<<343edd8c75b3182ea7f25f879afde048>>
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
  paymentAccountId?: string | null | undefined;
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
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "UpdatePurchaseSuccess";
    readonly result: {
      readonly id: string;
      readonly merchant: string;
      readonly occurredAt: string;
      readonly paymentAccountId: string | null | undefined;
      readonly revision: number;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
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
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "paymentAccountId",
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
        "abstractKey": null
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
    "cacheID": "ad19f96cd685c3daf2a1b25e1af85532",
    "id": null,
    "metadata": {},
    "name": "UpdatePurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation UpdatePurchaseMutation(\n  $input: UpdatePurchaseInput!\n) {\n  updatePurchase(input: $input) {\n    __typename\n    ... on UpdatePurchaseSuccess {\n      result {\n        id\n        revision\n        occurredAt\n        merchant\n        paymentAccountId\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "745433f732d5d56fce0c67e31f5bdff4";

export default node;

/**
 * @generated SignedSource<<45fde8ac7a401f2c0df11f3ac9e06046>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type PostPurchaseMutation$variables = {
  expectedRevision: number;
  idempotencyKey: string;
  purchaseId: string;
};
export type PostPurchaseMutation$data = {
  readonly postPurchase: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
      readonly revision: number;
      readonly status: PurchaseStatus;
    };
  };
};
export type PostPurchaseMutation = {
  response: PostPurchaseMutation$data;
  variables: PostPurchaseMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "expectedRevision"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "idempotencyKey"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "purchaseId"
},
v3 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "expectedRevision",
        "variableName": "expectedRevision"
      },
      {
        "kind": "Variable",
        "name": "idempotencyKey",
        "variableName": "idempotencyKey"
      },
      {
        "kind": "Variable",
        "name": "purchaseId",
        "variableName": "purchaseId"
      }
    ],
    "concreteType": null,
    "kind": "LinkedField",
    "name": "postPurchase",
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
                "name": "status",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "PostPurchaseSuccess",
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
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "PostPurchaseMutation",
    "selections": (v3/*:: as any*/),
    "type": "Mutation",
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
    "name": "PostPurchaseMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "c70c4b4c41ef05f8a22d62892c75326b",
    "id": null,
    "metadata": {},
    "name": "PostPurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation PostPurchaseMutation(\n  $purchaseId: ID!\n  $expectedRevision: Int!\n  $idempotencyKey: String!\n) {\n  postPurchase(purchaseId: $purchaseId, expectedRevision: $expectedRevision, idempotencyKey: $idempotencyKey) {\n    __typename\n    ... on PostPurchaseSuccess {\n      result {\n        id\n        revision\n        status\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "09de9834e8e652f8e2c82230fe1a906a";

export default node;

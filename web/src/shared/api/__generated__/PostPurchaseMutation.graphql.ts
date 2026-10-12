/**
 * @generated SignedSource<<f0866e48edcc6c7f29cd23eab2bb7e56>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type PostPurchaseMutation$variables = {
  expectedRevision: number;
  idempotencyKey: string;
  purchaseId: string;
};
export type PostPurchaseMutation$data = {
  readonly postPurchase: {
    readonly id: string;
    readonly revision: number;
    readonly status: PurchaseStatus;
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
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "postPurchase",
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
    "cacheID": "dd9b164a06ecde47cc12719ec09fd82a",
    "id": null,
    "metadata": {},
    "name": "PostPurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation PostPurchaseMutation(\n  $purchaseId: ID!\n  $expectedRevision: Int!\n  $idempotencyKey: String!\n) {\n  postPurchase(purchaseId: $purchaseId, expectedRevision: $expectedRevision, idempotencyKey: $idempotencyKey) {\n    id\n    revision\n    status\n  }\n}\n"
  }
};
})();

(node as any).hash = "fd7bb13f5b4f2ad498716838681700a9";

export default node;

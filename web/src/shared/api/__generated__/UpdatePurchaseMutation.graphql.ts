/**
 * @generated SignedSource<<b78524708f9860337e8056fe757934f7>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
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
};
export type UpdatePurchaseMutation$variables = {
  input: UpdatePurchaseInput;
};
export type UpdatePurchaseMutation$data = {
  readonly updatePurchase: {
    readonly id: string;
    readonly merchant: string;
    readonly occurredAt: string;
    readonly paymentAccountId: string | null | undefined;
    readonly revision: number;
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
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "updatePurchase",
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
    "cacheID": "a89070617ea935d6571f7cadc4defa96",
    "id": null,
    "metadata": {},
    "name": "UpdatePurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation UpdatePurchaseMutation(\n  $input: UpdatePurchaseInput!\n) {\n  updatePurchase(input: $input) {\n    id\n    revision\n    occurredAt\n    merchant\n    paymentAccountId\n  }\n}\n"
  }
};
})();

(node as any).hash = "b6996a853481ca15435e5f6ea321e4c8";

export default node;

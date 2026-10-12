/**
 * @generated SignedSource<<4a29afb674ecb4c147daa5158bd134d8>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
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
    readonly id: string;
    readonly revision: number;
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
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "correctPurchase",
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
    "cacheID": "149f7aba6701c3ab79c5bbc10e946481",
    "id": null,
    "metadata": {},
    "name": "CorrectPurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation CorrectPurchaseMutation(\n  $input: CorrectPurchaseInput!\n) {\n  correctPurchase(input: $input) {\n    id\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "d1e39f45cd0970d54368af9717b08758";

export default node;

/**
 * @generated SignedSource<<0188a180c9eeba6431fb274ead4f64a1>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type UpdatePurchaseItemInput = {
  allocations?: ReadonlyArray<AllocationInput> | null | undefined;
  amountYen?: any | null | undefined;
  baseRevision: number;
  categoryId?: string | null | undefined;
  changedFields: ReadonlyArray<string>;
  itemId: string;
  name?: string | null | undefined;
};
export type AllocationInput = {
  poolId: string;
  weight: any;
};
export type UpdatePurchaseItemMutation$variables = {
  input: UpdatePurchaseItemInput;
};
export type UpdatePurchaseItemMutation$data = {
  readonly updatePurchaseItem: {
    readonly amountYen: any;
    readonly categoryId: string | null | undefined;
    readonly id: string;
    readonly name: string;
    readonly revision: number;
  };
};
export type UpdatePurchaseItemMutation = {
  response: UpdatePurchaseItemMutation$data;
  variables: UpdatePurchaseItemMutation$variables;
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
    "concreteType": "PurchaseItem",
    "kind": "LinkedField",
    "name": "updatePurchaseItem",
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
        "name": "name",
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
        "name": "categoryId",
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
    "name": "UpdatePurchaseItemMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "UpdatePurchaseItemMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "ec3cd7a970b45a3f2c5074f14125f9e5",
    "id": null,
    "metadata": {},
    "name": "UpdatePurchaseItemMutation",
    "operationKind": "mutation",
    "text": "mutation UpdatePurchaseItemMutation(\n  $input: UpdatePurchaseItemInput!\n) {\n  updatePurchaseItem(input: $input) {\n    id\n    revision\n    name\n    amountYen\n    categoryId\n  }\n}\n"
  }
};
})();

(node as any).hash = "7ca542b3f9782e36b51f5c6032668c8f";

export default node;

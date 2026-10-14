/**
 * @generated SignedSource<<5cd6d744128c874928ca33498961c2a6>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseDefaultsQuery$variables = {
  householdId: string;
};
export type PurchaseDefaultsQuery$data = {
  readonly purchaseDefaults: ReadonlyArray<{
    readonly lastUsedAt: string;
    readonly merchant: string;
    readonly paymentAccountId: string;
    readonly purchaseCount: number;
  }>;
};
export type PurchaseDefaultsQuery = {
  response: PurchaseDefaultsQuery$data;
  variables: PurchaseDefaultsQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      }
    ],
    "concreteType": "PurchaseDefault",
    "kind": "LinkedField",
    "name": "purchaseDefaults",
    "plural": true,
    "selections": [
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
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "purchaseCount",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "lastUsedAt",
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
    "name": "PurchaseDefaultsQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "PurchaseDefaultsQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "fb120339e3d8d8589642f1519ff51b45",
    "id": null,
    "metadata": {},
    "name": "PurchaseDefaultsQuery",
    "operationKind": "query",
    "text": "query PurchaseDefaultsQuery(\n  $householdId: ID!\n) {\n  purchaseDefaults(householdId: $householdId) {\n    merchant\n    paymentAccountId\n    purchaseCount\n    lastUsedAt\n  }\n}\n"
  }
};
})();

(node as any).hash = "f00c83c08f5b59bb53e541504611d72a";

export default node;

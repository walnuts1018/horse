/**
 * @generated SignedSource<<d3049832d53764afb5c49ef7f43654a8>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SharedPurchaseDraftQuery$variables = {
  id: string;
};
export type SharedPurchaseDraftQuery$data = {
  readonly sharedPurchaseDraft: {
    readonly createdAt: string;
    readonly householdId: string;
    readonly id: string;
    readonly payload: string;
    readonly revision: number;
    readonly updatedAt: string;
  } | null | undefined;
};
export type SharedPurchaseDraftQuery = {
  response: SharedPurchaseDraftQuery$data;
  variables: SharedPurchaseDraftQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "id"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "id",
        "variableName": "id"
      }
    ],
    "concreteType": "SharedPurchaseDraft",
    "kind": "LinkedField",
    "name": "sharedPurchaseDraft",
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
        "name": "householdId",
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
        "name": "payload",
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
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "updatedAt",
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
    "name": "SharedPurchaseDraftQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SharedPurchaseDraftQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "a5155057320ca46f5b837a6531b200e9",
    "id": null,
    "metadata": {},
    "name": "SharedPurchaseDraftQuery",
    "operationKind": "query",
    "text": "query SharedPurchaseDraftQuery(\n  $id: ID!\n) {\n  sharedPurchaseDraft(id: $id) {\n    id\n    householdId\n    revision\n    payload\n    createdAt\n    updatedAt\n  }\n}\n"
  }
};
})();

(node as any).hash = "3aec9685902c535fa49027b8bed8994b";

export default node;

/**
 * @generated SignedSource<<81c8396c79c57f3a5f17eff8c71b0086>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type CreateSharedPurchaseDraftMutation$variables = {
  householdId: string;
  payload: string;
};
export type CreateSharedPurchaseDraftMutation$data = {
  readonly createSharedPurchaseDraft: {
    readonly createdAt: string;
    readonly householdId: string;
    readonly id: string;
    readonly payload: string;
    readonly revision: number;
    readonly updatedAt: string;
  };
};
export type CreateSharedPurchaseDraftMutation = {
  response: CreateSharedPurchaseDraftMutation$data;
  variables: CreateSharedPurchaseDraftMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "payload"
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
      },
      {
        "kind": "Variable",
        "name": "payload",
        "variableName": "payload"
      }
    ],
    "concreteType": "SharedPurchaseDraft",
    "kind": "LinkedField",
    "name": "createSharedPurchaseDraft",
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
    "name": "CreateSharedPurchaseDraftMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateSharedPurchaseDraftMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "e22fbd9e0bb9ea98355619eadc47d07e",
    "id": null,
    "metadata": {},
    "name": "CreateSharedPurchaseDraftMutation",
    "operationKind": "mutation",
    "text": "mutation CreateSharedPurchaseDraftMutation(\n  $householdId: ID!\n  $payload: String!\n) {\n  createSharedPurchaseDraft(householdId: $householdId, payload: $payload) {\n    id\n    householdId\n    revision\n    payload\n    createdAt\n    updatedAt\n  }\n}\n"
  }
};
})();

(node as any).hash = "da247d7ad85efd67220d8d375cffdbea";

export default node;

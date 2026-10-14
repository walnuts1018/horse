/**
 * @generated SignedSource<<12d2d8433362ad6e8dc0c511cd3ed621>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type UpdateSharedPurchaseDraftMutation$variables = {
  expectedRevision: number;
  id: string;
  payload: string;
};
export type UpdateSharedPurchaseDraftMutation$data = {
  readonly updateSharedPurchaseDraft: {
    readonly conflict: boolean;
    readonly draft: {
      readonly createdAt: string;
      readonly householdId: string;
      readonly id: string;
      readonly payload: string;
      readonly revision: number;
      readonly updatedAt: string;
    };
  };
};
export type UpdateSharedPurchaseDraftMutation = {
  response: UpdateSharedPurchaseDraftMutation$data;
  variables: UpdateSharedPurchaseDraftMutation$variables;
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
  "name": "id"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "payload"
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
        "name": "id",
        "variableName": "id"
      },
      {
        "kind": "Variable",
        "name": "payload",
        "variableName": "payload"
      }
    ],
    "concreteType": "UpdateSharedPurchaseDraftPayload",
    "kind": "LinkedField",
    "name": "updateSharedPurchaseDraft",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "conflict",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "SharedPurchaseDraft",
        "kind": "LinkedField",
        "name": "draft",
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
    "name": "UpdateSharedPurchaseDraftMutation",
    "selections": (v3/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "UpdateSharedPurchaseDraftMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "600e5df5c54fc44b1d653407c5edb616",
    "id": null,
    "metadata": {},
    "name": "UpdateSharedPurchaseDraftMutation",
    "operationKind": "mutation",
    "text": "mutation UpdateSharedPurchaseDraftMutation(\n  $id: ID!\n  $expectedRevision: Int!\n  $payload: String!\n) {\n  updateSharedPurchaseDraft(id: $id, expectedRevision: $expectedRevision, payload: $payload) {\n    conflict\n    draft {\n      id\n      householdId\n      revision\n      payload\n      createdAt\n      updatedAt\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "86a5a28fdc7748aaab2aff33e158623c";

export default node;

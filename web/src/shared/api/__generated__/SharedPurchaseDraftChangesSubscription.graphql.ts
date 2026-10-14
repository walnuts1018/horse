/**
 * @generated SignedSource<<7e136081a268d53ab7c58c43162b1feb>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SharedPurchaseDraftChangesSubscription$variables = {
  after?: string | null | undefined;
  id: string;
};
export type SharedPurchaseDraftChangesSubscription$data = {
  readonly sharedPurchaseDraftChanges: {
    readonly cursor: string;
  };
};
export type SharedPurchaseDraftChangesSubscription = {
  response: SharedPurchaseDraftChangesSubscription$data;
  variables: SharedPurchaseDraftChangesSubscription$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "after"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "id"
},
v2 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "after",
        "variableName": "after"
      },
      {
        "kind": "Variable",
        "name": "id",
        "variableName": "id"
      }
    ],
    "concreteType": "SharedPurchaseDraftChange",
    "kind": "LinkedField",
    "name": "sharedPurchaseDraftChanges",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "cursor",
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
      (v1/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "SharedPurchaseDraftChangesSubscription",
    "selections": (v2/*:: as any*/),
    "type": "Subscription",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "SharedPurchaseDraftChangesSubscription",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "ce8d235ab25cf85eed4017d944aab5f3",
    "id": null,
    "metadata": {},
    "name": "SharedPurchaseDraftChangesSubscription",
    "operationKind": "subscription",
    "text": "subscription SharedPurchaseDraftChangesSubscription(\n  $id: ID!\n  $after: String\n) {\n  sharedPurchaseDraftChanges(id: $id, after: $after) {\n    cursor\n  }\n}\n"
  }
};
})();

(node as any).hash = "401375e38e63363321d3b1be1c9e476e";

export default node;

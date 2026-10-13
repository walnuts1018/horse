/**
 * @generated SignedSource<<641843a58ce6c23a813557cf17d0ae14>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseChangesSubscription$variables = {
  after?: string | null | undefined;
  householdId: string;
};
export type PurchaseChangesSubscription$data = {
  readonly purchaseChanges: {
    readonly cursor: string;
  };
};
export type PurchaseChangesSubscription = {
  response: PurchaseChangesSubscription$data;
  variables: PurchaseChangesSubscription$variables;
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
  "name": "householdId"
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
        "name": "householdId",
        "variableName": "householdId"
      }
    ],
    "concreteType": "PurchaseChange",
    "kind": "LinkedField",
    "name": "purchaseChanges",
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
    "name": "PurchaseChangesSubscription",
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
    "name": "PurchaseChangesSubscription",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "b67aef1dd19f9ee9fcbaa2ba0c56e3d9",
    "id": null,
    "metadata": {},
    "name": "PurchaseChangesSubscription",
    "operationKind": "subscription",
    "text": "subscription PurchaseChangesSubscription(\n  $householdId: ID!\n  $after: String\n) {\n  purchaseChanges(householdId: $householdId, after: $after) {\n    cursor\n  }\n}\n"
  }
};
})();

(node as any).hash = "7e83767f738d71198a5c97d6800e5559";

export default node;

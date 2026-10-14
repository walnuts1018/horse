/**
 * @generated SignedSource<<30c053e2da79f8d6db524233145e6143>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseChangeEntity = "PURCHASE" | "PURCHASE_ITEM" | "%future added value";
export type PurchaseChangeField = "ALLOCATIONS" | "AMOUNT_YEN" | "CATEGORY_ID" | "CORRECTION" | "MERCHANT" | "NAME" | "OCCURRED_AT" | "PAYMENT_ALLOCATIONS" | "PRINTED_UNIT_PRICE" | "PRODUCT_VARIANT_ID" | "QUANTITY" | "STATUS" | "TAGS" | "%future added value";
export type PurchaseChangeKind = "CORRECTED" | "CREATED" | "ITEM_UPDATED" | "POSTED" | "UPDATED" | "%future added value";
export type PurchaseChangesSubscription$variables = {
  after?: string | null | undefined;
  householdId: string;
};
export type PurchaseChangesSubscription$data = {
  readonly purchaseChanges: {
    readonly changedFields: ReadonlyArray<PurchaseChangeField>;
    readonly cursor: string;
    readonly entityId: string;
    readonly entityKind: PurchaseChangeEntity;
    readonly kind: PurchaseChangeKind;
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
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "entityId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "entityKind",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "kind",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "changedFields",
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
    "cacheID": "e8475f4a0f968afc81051878957f8b44",
    "id": null,
    "metadata": {},
    "name": "PurchaseChangesSubscription",
    "operationKind": "subscription",
    "text": "subscription PurchaseChangesSubscription(\n  $householdId: ID!\n  $after: String\n) {\n  purchaseChanges(householdId: $householdId, after: $after) {\n    cursor\n    entityId\n    entityKind\n    kind\n    changedFields\n  }\n}\n"
  }
};
})();

(node as any).hash = "d260bdd49deeacd9f47df1d91923d164";

export default node;

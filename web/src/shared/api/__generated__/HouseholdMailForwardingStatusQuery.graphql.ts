/**
 * @generated SignedSource<<57d3f07044e86de603edc2ba5c35e1fc>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HouseholdMailForwardingStatusQuery$variables = {
  householdId: string;
};
export type HouseholdMailForwardingStatusQuery$data = {
  readonly householdMailForwardingStatus: {
    readonly active: boolean;
    readonly createdAt: string;
  } | null | undefined;
};
export type HouseholdMailForwardingStatusQuery = {
  response: HouseholdMailForwardingStatusQuery$data;
  variables: HouseholdMailForwardingStatusQuery$variables;
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
    "concreteType": "HouseholdMailForwardingStatus",
    "kind": "LinkedField",
    "name": "householdMailForwardingStatus",
    "plural": false,
    "selections": [
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
        "name": "active",
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
    "name": "HouseholdMailForwardingStatusQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "HouseholdMailForwardingStatusQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "54b54f24d24c5413dcf66eeb795e851d",
    "id": null,
    "metadata": {},
    "name": "HouseholdMailForwardingStatusQuery",
    "operationKind": "query",
    "text": "query HouseholdMailForwardingStatusQuery(\n  $householdId: ID!\n) {\n  householdMailForwardingStatus(householdId: $householdId) {\n    createdAt\n    active\n  }\n}\n"
  }
};
})();

(node as any).hash = "67e2519a2c9a157be49f12677d9f790a";

export default node;

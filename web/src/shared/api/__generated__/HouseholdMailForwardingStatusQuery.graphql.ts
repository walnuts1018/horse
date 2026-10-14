/**
 * @generated SignedSource<<4682d0e0b321cd0c005a7b0b407b1893>>
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
  readonly householdMailForwarding: {
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
    "name": "householdMailForwarding",
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
    "cacheID": "664faee788475a96a00b8bd28795cf62",
    "id": null,
    "metadata": {},
    "name": "HouseholdMailForwardingStatusQuery",
    "operationKind": "query",
    "text": "query HouseholdMailForwardingStatusQuery(\n  $householdId: ID!\n) {\n  householdMailForwarding(householdId: $householdId) {\n    createdAt\n    active\n  }\n}\n"
  }
};
})();

(node as any).hash = "abb13c41c3e157ba5a511666f51f10bb";

export default node;

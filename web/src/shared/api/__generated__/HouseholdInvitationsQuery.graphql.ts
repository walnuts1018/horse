/**
 * @generated SignedSource<<0b13da802cd7ecf264af714571fd84bb>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HouseholdInvitationsQuery$variables = {
  householdId: string;
};
export type HouseholdInvitationsQuery$data = {
  readonly householdInvitations: ReadonlyArray<{
    readonly acceptedAt: string | null | undefined;
    readonly createdAt: string;
    readonly expiresAt: string;
    readonly householdId: string;
    readonly id: string;
    readonly revokedAt: string | null | undefined;
    readonly role: string;
  }>;
};
export type HouseholdInvitationsQuery = {
  response: HouseholdInvitationsQuery$data;
  variables: HouseholdInvitationsQuery$variables;
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
    "concreteType": "HouseholdInvitation",
    "kind": "LinkedField",
    "name": "householdInvitations",
    "plural": true,
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
        "name": "role",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "expiresAt",
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
        "name": "revokedAt",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "acceptedAt",
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
    "name": "HouseholdInvitationsQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "HouseholdInvitationsQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "7d972170b6a597038e9f78a005a8375d",
    "id": null,
    "metadata": {},
    "name": "HouseholdInvitationsQuery",
    "operationKind": "query",
    "text": "query HouseholdInvitationsQuery(\n  $householdId: ID!\n) {\n  householdInvitations(householdId: $householdId) {\n    id\n    householdId\n    role\n    expiresAt\n    createdAt\n    revokedAt\n    acceptedAt\n  }\n}\n"
  }
};
})();

(node as any).hash = "9b7634a2e9523e418f5f5c15b936f86b";

export default node;

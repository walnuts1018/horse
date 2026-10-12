/**
 * @generated SignedSource<<cbb33601d0f11d949b99ad260a818a79>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HouseholdInvitationStatus = "ACCEPTED" | "EXPIRED" | "PENDING" | "REVOKED" | "%future added value";
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
    readonly status: HouseholdInvitationStatus;
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
        "name": "status",
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
    "cacheID": "2d2da2744a2e20b857c31a1f489751df",
    "id": null,
    "metadata": {},
    "name": "HouseholdInvitationsQuery",
    "operationKind": "query",
    "text": "query HouseholdInvitationsQuery(\n  $householdId: ID!\n) {\n  householdInvitations(householdId: $householdId) {\n    id\n    householdId\n    role\n    status\n    expiresAt\n    createdAt\n    revokedAt\n    acceptedAt\n  }\n}\n"
  }
};
})();

(node as any).hash = "142de40e50ad3fb20c656a7619380a8b";

export default node;

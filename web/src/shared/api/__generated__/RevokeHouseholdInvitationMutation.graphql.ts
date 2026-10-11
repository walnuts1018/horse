/**
 * @generated SignedSource<<d2a98bb8b63c824a0057bcd14e6c1a06>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type RevokeHouseholdInvitationMutation$variables = {
  householdId: string;
  invitationId: string;
};
export type RevokeHouseholdInvitationMutation$data = {
  readonly revokeHouseholdInvitation: {
    readonly id: string;
    readonly revokedAt: string | null | undefined;
  };
};
export type RevokeHouseholdInvitationMutation = {
  response: RevokeHouseholdInvitationMutation$data;
  variables: RevokeHouseholdInvitationMutation$variables;
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
    "name": "invitationId"
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
        "name": "invitationId",
        "variableName": "invitationId"
      }
    ],
    "concreteType": "HouseholdInvitation",
    "kind": "LinkedField",
    "name": "revokeHouseholdInvitation",
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
        "name": "revokedAt",
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
    "name": "RevokeHouseholdInvitationMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RevokeHouseholdInvitationMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "ac9f96452ea1a219cf418288e0e80497",
    "id": null,
    "metadata": {},
    "name": "RevokeHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation RevokeHouseholdInvitationMutation(\n  $householdId: ID!\n  $invitationId: ID!\n) {\n  revokeHouseholdInvitation(householdId: $householdId, invitationId: $invitationId) {\n    id\n    revokedAt\n  }\n}\n"
  }
};
})();

(node as any).hash = "9b6123ddd5352f28dff519dad3673396";

export default node;

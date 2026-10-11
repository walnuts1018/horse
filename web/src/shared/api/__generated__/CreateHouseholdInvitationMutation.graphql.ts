/**
 * @generated SignedSource<<fbcb181d8551191d3dd039ad56edc70c>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type CreateHouseholdInvitationInput = {
  expiresAt: string;
  householdId: string;
};
export type CreateHouseholdInvitationMutation$variables = {
  input: CreateHouseholdInvitationInput;
};
export type CreateHouseholdInvitationMutation$data = {
  readonly createHouseholdInvitation: {
    readonly invitation: {
      readonly acceptedAt: string | null | undefined;
      readonly createdAt: string;
      readonly expiresAt: string;
      readonly householdId: string;
      readonly id: string;
      readonly revokedAt: string | null | undefined;
      readonly role: string;
    };
    readonly token: string;
  };
};
export type CreateHouseholdInvitationMutation = {
  response: CreateHouseholdInvitationMutation$data;
  variables: CreateHouseholdInvitationMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "input"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "input",
        "variableName": "input"
      }
    ],
    "concreteType": "CreatedHouseholdInvitation",
    "kind": "LinkedField",
    "name": "createHouseholdInvitation",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "token",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "HouseholdInvitation",
        "kind": "LinkedField",
        "name": "invitation",
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
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "CreateHouseholdInvitationMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateHouseholdInvitationMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "fb0856ccd91c6c229fc0d67c24fdc452",
    "id": null,
    "metadata": {},
    "name": "CreateHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation CreateHouseholdInvitationMutation(\n  $input: CreateHouseholdInvitationInput!\n) {\n  createHouseholdInvitation(input: $input) {\n    token\n    invitation {\n      id\n      householdId\n      role\n      expiresAt\n      createdAt\n      revokedAt\n      acceptedAt\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "82a73c7db5610ae1b7be34fe088b4a64";

export default node;

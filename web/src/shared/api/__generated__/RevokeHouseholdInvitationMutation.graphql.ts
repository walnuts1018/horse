/**
 * @generated SignedSource<<4eb347177a80706e12e24c398d633422>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HouseholdInvitationStatus = "ACCEPTED" | "EXPIRED" | "PENDING" | "REVOKED" | "%future added value";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type RevokeHouseholdInvitationMutation$variables = {
  householdId: string;
  invitationId: string;
};
export type RevokeHouseholdInvitationMutation$data = {
  readonly revokeHouseholdInvitation: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
      readonly revokedAt: string | null | undefined;
      readonly status: HouseholdInvitationStatus;
    };
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
    "concreteType": null,
    "kind": "LinkedField",
    "name": "revokeHouseholdInvitation",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "__typename",
        "storageKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "HouseholdInvitation",
            "kind": "LinkedField",
            "name": "result",
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
                "name": "status",
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
        ],
        "type": "RevokeHouseholdInvitationSuccess",
        "abstractKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "code",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "message",
            "storageKey": null
          }
        ],
        "type": "MutationError",
        "abstractKey": "__isMutationError"
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
    "cacheID": "d43651255a91f97a8cc06050596ffb79",
    "id": null,
    "metadata": {},
    "name": "RevokeHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation RevokeHouseholdInvitationMutation(\n  $householdId: ID!\n  $invitationId: ID!\n) {\n  revokeHouseholdInvitation(householdId: $householdId, invitationId: $invitationId) {\n    __typename\n    ... on RevokeHouseholdInvitationSuccess {\n      result {\n        id\n        status\n        revokedAt\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "aba3e2f49b1cd259d6acba163dfe99a0";

export default node;

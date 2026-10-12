/**
 * @generated SignedSource<<2acc9192af32231d8f6cee0fe50eda24>>
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
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "RevokeHouseholdInvitationSuccess";
    readonly result: {
      readonly id: string;
      readonly revokedAt: string | null | undefined;
      readonly status: HouseholdInvitationStatus;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
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
        "abstractKey": null
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
    "cacheID": "3128ba4dcbefd20f0f04b09d8ccd9e9b",
    "id": null,
    "metadata": {},
    "name": "RevokeHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation RevokeHouseholdInvitationMutation(\n  $householdId: ID!\n  $invitationId: ID!\n) {\n  revokeHouseholdInvitation(householdId: $householdId, invitationId: $invitationId) {\n    __typename\n    ... on RevokeHouseholdInvitationSuccess {\n      result {\n        id\n        status\n        revokedAt\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "aba3e2f49b1cd259d6acba163dfe99a0";

export default node;

/**
 * @generated SignedSource<<ca8db2b69131ab3ece04efe0b0750f3f>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
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
    "cacheID": "a28351db551e3d47f80fadcdb48f214a",
    "id": null,
    "metadata": {},
    "name": "RevokeHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation RevokeHouseholdInvitationMutation(\n  $householdId: ID!\n  $invitationId: ID!\n) {\n  revokeHouseholdInvitation(householdId: $householdId, invitationId: $invitationId) {\n    __typename\n    ... on RevokeHouseholdInvitationSuccess {\n      result {\n        id\n        revokedAt\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "0ca805d4e415d569194c53752c5d5e08";

export default node;

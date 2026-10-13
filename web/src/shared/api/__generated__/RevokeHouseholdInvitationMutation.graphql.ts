/**
 * @generated SignedSource<<fb734e3c7852fd37562c68319b1b3204>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HouseholdInvitationStatus = "ACCEPTED" | "EXPIRED" | "PENDING" | "REVOKED" | "%future added value";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type RevokeHouseholdInvitationInput = {
  householdId: string;
  invitationId: string;
};
export type RevokeHouseholdInvitationMutation$variables = {
  input: RevokeHouseholdInvitationInput;
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
    "cacheID": "2b3a149b990d1176634b8e5063b5b169",
    "id": null,
    "metadata": {},
    "name": "RevokeHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation RevokeHouseholdInvitationMutation(\n  $input: RevokeHouseholdInvitationInput!\n) {\n  revokeHouseholdInvitation(input: $input) {\n    __typename\n    ... on RevokeHouseholdInvitationSuccess {\n      result {\n        id\n        status\n        revokedAt\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "ecbbc1722d373f073206e9f856871501";

export default node;

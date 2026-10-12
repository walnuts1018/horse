/**
 * @generated SignedSource<<faab4b13ede8b18d88bcf26f4433bb1b>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HouseholdInvitationStatus = "ACCEPTED" | "EXPIRED" | "PENDING" | "REVOKED" | "%future added value";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type CreateHouseholdInvitationInput = {
  expiresAt: string;
  householdId: string;
};
export type CreateHouseholdInvitationMutation$variables = {
  input: CreateHouseholdInvitationInput;
};
export type CreateHouseholdInvitationMutation$data = {
  readonly createHouseholdInvitation: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly invitation: {
        readonly acceptedAt: string | null | undefined;
        readonly createdAt: string;
        readonly expiresAt: string;
        readonly householdId: string;
        readonly id: string;
        readonly revokedAt: string | null | undefined;
        readonly role: string;
        readonly status: HouseholdInvitationStatus;
      };
      readonly token: string;
    };
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
    "concreteType": null,
    "kind": "LinkedField",
    "name": "createHouseholdInvitation",
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
            "concreteType": "CreatedHouseholdInvitation",
            "kind": "LinkedField",
            "name": "result",
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
            ],
            "storageKey": null
          }
        ],
        "type": "CreateHouseholdInvitationSuccess",
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
    "cacheID": "02d38680b59ec435599c4d3c13c6db30",
    "id": null,
    "metadata": {},
    "name": "CreateHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation CreateHouseholdInvitationMutation(\n  $input: CreateHouseholdInvitationInput!\n) {\n  createHouseholdInvitation(input: $input) {\n    __typename\n    ... on CreateHouseholdInvitationSuccess {\n      result {\n        token\n        invitation {\n          id\n          householdId\n          role\n          status\n          expiresAt\n          createdAt\n          revokedAt\n          acceptedAt\n        }\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "7fd18b7806a3fb1a8ba3f4bfd86ced7d";

export default node;

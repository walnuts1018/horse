/**
 * @generated SignedSource<<c5c7cd26656819cd38f07e44da2e8602>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
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
        readonly __typename: string;
        readonly createdAt: string;
        readonly expiresAt: string;
        readonly householdId: string;
        readonly id: string;
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
v1 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v2 = [
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
      (v1/*:: as any*/),
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
                "concreteType": null,
                "kind": "LinkedField",
                "name": "invitation",
                "plural": false,
                "selections": [
                  (v1/*:: as any*/),
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
                    "name": "expiresAt",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "createdAt",
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
    "selections": (v2/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateHouseholdInvitationMutation",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "aa1abdd9347dd98a9ddc8e1b8c4b555f",
    "id": null,
    "metadata": {},
    "name": "CreateHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation CreateHouseholdInvitationMutation(\n  $input: CreateHouseholdInvitationInput!\n) {\n  createHouseholdInvitation(input: $input) {\n    __typename\n    ... on CreateHouseholdInvitationSuccess {\n      result {\n        token\n        invitation {\n          __typename\n          id\n          householdId\n          expiresAt\n          createdAt\n        }\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "90b727beba76dc8fc32899e6042d6409";

export default node;

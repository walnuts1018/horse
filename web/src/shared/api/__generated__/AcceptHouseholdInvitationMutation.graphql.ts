/**
 * @generated SignedSource<<8f484d8f2bebf993abc53db3dea9ee44>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type AcceptHouseholdInvitationMutation$variables = {
  token: string;
};
export type AcceptHouseholdInvitationMutation$data = {
  readonly acceptHouseholdInvitation: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
      readonly name: string;
    };
  };
};
export type AcceptHouseholdInvitationMutation = {
  response: AcceptHouseholdInvitationMutation$data;
  variables: AcceptHouseholdInvitationMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "token"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "token",
        "variableName": "token"
      }
    ],
    "concreteType": null,
    "kind": "LinkedField",
    "name": "acceptHouseholdInvitation",
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
            "concreteType": "Household",
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
                "name": "name",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "AcceptHouseholdInvitationSuccess",
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
    "name": "AcceptHouseholdInvitationMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AcceptHouseholdInvitationMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "7bc3c712b364635e1763a1dfa48bb1bb",
    "id": null,
    "metadata": {},
    "name": "AcceptHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation AcceptHouseholdInvitationMutation(\n  $token: String!\n) {\n  acceptHouseholdInvitation(token: $token) {\n    __typename\n    ... on AcceptHouseholdInvitationSuccess {\n      result {\n        id\n        name\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "585e8e92909024df0f2dd8fde08c33ab";

export default node;

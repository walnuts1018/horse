/**
 * @generated SignedSource<<f06ff9b766aea5c80ff706b0d0f4ec60>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type HouseholdMailForwardingInput = {
  householdId: string;
};
export type RevokeHouseholdMailForwardingMutation$variables = {
  input: HouseholdMailForwardingInput;
};
export type RevokeHouseholdMailForwardingMutation$data = {
  readonly revokeHouseholdMailForwarding: {
    readonly __typename: "HouseholdMailForwardingUserError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "RevokeHouseholdMailForwardingSuccess";
    readonly result: {
      readonly active: boolean;
      readonly createdAt: string;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type RevokeHouseholdMailForwardingMutation = {
  response: RevokeHouseholdMailForwardingMutation$data;
  variables: RevokeHouseholdMailForwardingMutation$variables;
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
    "name": "revokeHouseholdMailForwarding",
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
            "concreteType": "HouseholdMailForwardingStatus",
            "kind": "LinkedField",
            "name": "result",
            "plural": false,
            "selections": [
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
                "name": "active",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "RevokeHouseholdMailForwardingSuccess",
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
        "type": "HouseholdMailForwardingUserError",
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
    "name": "RevokeHouseholdMailForwardingMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RevokeHouseholdMailForwardingMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "64a05949a6dbe87a356d8a57b0a9eee0",
    "id": null,
    "metadata": {},
    "name": "RevokeHouseholdMailForwardingMutation",
    "operationKind": "mutation",
    "text": "mutation RevokeHouseholdMailForwardingMutation(\n  $input: HouseholdMailForwardingInput!\n) {\n  revokeHouseholdMailForwarding(input: $input) {\n    __typename\n    ... on RevokeHouseholdMailForwardingSuccess {\n      result {\n        createdAt\n        active\n      }\n    }\n    ... on HouseholdMailForwardingUserError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "7811b8665f1d4072066845f9ee4e3522";

export default node;

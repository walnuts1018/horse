/**
 * @generated SignedSource<<da5e2141b98cfc9fdba93a1b5c8a92fd>>
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
export type RotateHouseholdMailForwardingMutation$variables = {
  input: HouseholdMailForwardingInput;
};
export type RotateHouseholdMailForwardingMutation$data = {
  readonly rotateHouseholdMailForwarding: {
    readonly __typename: "HouseholdMailForwardingUserError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "RotateHouseholdMailForwardingSuccess";
    readonly result: {
      readonly address: string;
      readonly status: {
        readonly createdAt: string;
      };
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type RotateHouseholdMailForwardingMutation = {
  response: RotateHouseholdMailForwardingMutation$data;
  variables: RotateHouseholdMailForwardingMutation$variables;
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
    "name": "rotateHouseholdMailForwarding",
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
            "concreteType": "HouseholdMailForwardingAddress",
            "kind": "LinkedField",
            "name": "result",
            "plural": false,
            "selections": [
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "address",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "concreteType": "HouseholdMailForwardingStatus",
                "kind": "LinkedField",
                "name": "status",
                "plural": false,
                "selections": [
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
        "type": "RotateHouseholdMailForwardingSuccess",
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
    "name": "RotateHouseholdMailForwardingMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RotateHouseholdMailForwardingMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "90a371703a09408e5e46f39792ed91c6",
    "id": null,
    "metadata": {},
    "name": "RotateHouseholdMailForwardingMutation",
    "operationKind": "mutation",
    "text": "mutation RotateHouseholdMailForwardingMutation(\n  $input: HouseholdMailForwardingInput!\n) {\n  rotateHouseholdMailForwarding(input: $input) {\n    __typename\n    ... on RotateHouseholdMailForwardingSuccess {\n      result {\n        address\n        status {\n          createdAt\n        }\n      }\n    }\n    ... on HouseholdMailForwardingUserError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "e9f7449b84218b0d8c417d9d224a799a";

export default node;

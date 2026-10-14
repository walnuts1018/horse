/**
 * @generated SignedSource<<7d77862087258de3c1a467e61918b716>>
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
export type IssueHouseholdMailForwardingMutation$variables = {
  input: HouseholdMailForwardingInput;
};
export type IssueHouseholdMailForwardingMutation$data = {
  readonly issueHouseholdMailForwarding: {
    readonly __typename: "HouseholdMailForwardingUserError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "IssueHouseholdMailForwardingSuccess";
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
export type IssueHouseholdMailForwardingMutation = {
  response: IssueHouseholdMailForwardingMutation$data;
  variables: IssueHouseholdMailForwardingMutation$variables;
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
    "name": "issueHouseholdMailForwarding",
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
        "type": "IssueHouseholdMailForwardingSuccess",
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
    "name": "IssueHouseholdMailForwardingMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "IssueHouseholdMailForwardingMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "dd632119c73f35010c4b6ec6a731b2ec",
    "id": null,
    "metadata": {},
    "name": "IssueHouseholdMailForwardingMutation",
    "operationKind": "mutation",
    "text": "mutation IssueHouseholdMailForwardingMutation(\n  $input: HouseholdMailForwardingInput!\n) {\n  issueHouseholdMailForwarding(input: $input) {\n    __typename\n    ... on IssueHouseholdMailForwardingSuccess {\n      result {\n        address\n        status {\n          createdAt\n        }\n      }\n    }\n    ... on HouseholdMailForwardingUserError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "028263b054608525bc728e5ab6bbf66b";

export default node;

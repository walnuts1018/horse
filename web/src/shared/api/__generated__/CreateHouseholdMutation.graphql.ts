/**
 * @generated SignedSource<<e297c57e3c4ed64b1456d5f0892e3af5>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type CreateHouseholdInput = {
  memberNames: ReadonlyArray<string>;
  name: string;
};
export type CreateHouseholdMutation$variables = {
  input: CreateHouseholdInput;
};
export type CreateHouseholdMutation$data = {
  readonly createHousehold: {
    readonly __typename: "CreateHouseholdSuccess";
    readonly result: {
      readonly id: string;
    };
  } | {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type CreateHouseholdMutation = {
  response: CreateHouseholdMutation$data;
  variables: CreateHouseholdMutation$variables;
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
    "name": "createHousehold",
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
              }
            ],
            "storageKey": null
          }
        ],
        "type": "CreateHouseholdSuccess",
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
    "name": "CreateHouseholdMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateHouseholdMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "3736dd5d55d7d9a9ed65ecbac4f33495",
    "id": null,
    "metadata": {},
    "name": "CreateHouseholdMutation",
    "operationKind": "mutation",
    "text": "mutation CreateHouseholdMutation(\n  $input: CreateHouseholdInput!\n) {\n  createHousehold(input: $input) {\n    __typename\n    ... on CreateHouseholdSuccess {\n      result {\n        id\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "cc3ce2a3de369a76b541deb586df3cf0";

export default node;

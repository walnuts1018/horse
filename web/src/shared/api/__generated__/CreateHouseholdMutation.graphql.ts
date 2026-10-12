/**
 * @generated SignedSource<<9db9276cd9ceb32f4faaeda1bff72104>>
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
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
    };
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
    "cacheID": "f2ad79ef0b52d6614ca70e2c1d18ef2d",
    "id": null,
    "metadata": {},
    "name": "CreateHouseholdMutation",
    "operationKind": "mutation",
    "text": "mutation CreateHouseholdMutation(\n  $input: CreateHouseholdInput!\n) {\n  createHousehold(input: $input) {\n    __typename\n    ... on CreateHouseholdSuccess {\n      result {\n        id\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "cc3ce2a3de369a76b541deb586df3cf0";

export default node;

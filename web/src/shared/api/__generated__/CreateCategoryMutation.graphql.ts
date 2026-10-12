/**
 * @generated SignedSource<<a2f651f94ccbc64756479cc82580a051>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type CreateCategoryInput = {
  householdId: string;
  name: string;
  parentId?: string | null | undefined;
};
export type CreateCategoryMutation$variables = {
  input: CreateCategoryInput;
};
export type CreateCategoryMutation$data = {
  readonly createCategory: {
    readonly __typename: "CreateCategorySuccess";
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
export type CreateCategoryMutation = {
  response: CreateCategoryMutation$data;
  variables: CreateCategoryMutation$variables;
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
    "name": "createCategory",
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
            "concreteType": "Category",
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
        "type": "CreateCategorySuccess",
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
    "name": "CreateCategoryMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateCategoryMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "f6e3c61ce9db4a6b85fafba89784361f",
    "id": null,
    "metadata": {},
    "name": "CreateCategoryMutation",
    "operationKind": "mutation",
    "text": "mutation CreateCategoryMutation(\n  $input: CreateCategoryInput!\n) {\n  createCategory(input: $input) {\n    __typename\n    ... on CreateCategorySuccess {\n      result {\n        id\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "955ea11fa86a8f8675ee7fa8f2d740f4";

export default node;

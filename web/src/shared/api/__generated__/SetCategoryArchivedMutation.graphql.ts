/**
 * @generated SignedSource<<e8f11c5f4d96a558793338abe3819a36>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SetCategoryArchivedInput = {
  archived: boolean;
  categoryId: string;
};
export type SetCategoryArchivedMutation$variables = {
  input: SetCategoryArchivedInput;
};
export type SetCategoryArchivedMutation$data = {
  readonly setCategoryArchived: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly archived: boolean;
      readonly id: string;
    };
  };
};
export type SetCategoryArchivedMutation = {
  response: SetCategoryArchivedMutation$data;
  variables: SetCategoryArchivedMutation$variables;
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
    "name": "setCategoryArchived",
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
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "archived",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "SetCategoryArchivedSuccess",
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
    "name": "SetCategoryArchivedMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetCategoryArchivedMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "23b0f783ec39bfa44994334b1e9e167d",
    "id": null,
    "metadata": {},
    "name": "SetCategoryArchivedMutation",
    "operationKind": "mutation",
    "text": "mutation SetCategoryArchivedMutation(\n  $input: SetCategoryArchivedInput!\n) {\n  setCategoryArchived(input: $input) {\n    __typename\n    ... on SetCategoryArchivedSuccess {\n      result {\n        id\n        archived\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "02647cb9d997d5acb10b3811391f9e97";

export default node;

/**
 * @generated SignedSource<<204d3cc9b9d48b5f79c530c4a263ec94>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SetCategoryArchivedMutation$variables = {
  archived: boolean;
  categoryId: string;
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
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "archived"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "categoryId"
},
v2 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "archived",
        "variableName": "archived"
      },
      {
        "kind": "Variable",
        "name": "categoryId",
        "variableName": "categoryId"
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
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "SetCategoryArchivedMutation",
    "selections": (v2/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "SetCategoryArchivedMutation",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "25ae16af5a1a59dc9cde41c4ddecbdf6",
    "id": null,
    "metadata": {},
    "name": "SetCategoryArchivedMutation",
    "operationKind": "mutation",
    "text": "mutation SetCategoryArchivedMutation(\n  $categoryId: ID!\n  $archived: Boolean!\n) {\n  setCategoryArchived(categoryId: $categoryId, archived: $archived) {\n    __typename\n    ... on SetCategoryArchivedSuccess {\n      result {\n        id\n        archived\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "c66701127da7408d2641152fa802ac48";

export default node;

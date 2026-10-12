/**
 * @generated SignedSource<<718c3b513b5608526af3e1e27046cbb9>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type ArchiveBudgetMutation$variables = {
  archived: boolean;
  baseRevision: number;
  budgetId: string;
};
export type ArchiveBudgetMutation$data = {
  readonly archiveBudget: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly archived: boolean;
      readonly id: string;
      readonly revision: number;
    };
  };
};
export type ArchiveBudgetMutation = {
  response: ArchiveBudgetMutation$data;
  variables: ArchiveBudgetMutation$variables;
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
  "name": "baseRevision"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "budgetId"
},
v3 = [
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
        "name": "baseRevision",
        "variableName": "baseRevision"
      },
      {
        "kind": "Variable",
        "name": "budgetId",
        "variableName": "budgetId"
      }
    ],
    "concreteType": null,
    "kind": "LinkedField",
    "name": "archiveBudget",
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
            "concreteType": "Budget",
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
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "revision",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "ArchiveBudgetSuccess",
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
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "ArchiveBudgetMutation",
    "selections": (v3/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "ArchiveBudgetMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "afada52118a51f682336eb58f89aa62b",
    "id": null,
    "metadata": {},
    "name": "ArchiveBudgetMutation",
    "operationKind": "mutation",
    "text": "mutation ArchiveBudgetMutation(\n  $budgetId: ID!\n  $baseRevision: Int!\n  $archived: Boolean!\n) {\n  archiveBudget(budgetId: $budgetId, baseRevision: $baseRevision, archived: $archived) {\n    __typename\n    ... on ArchiveBudgetSuccess {\n      result {\n        id\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "bfe5ce5245062026fb2d2e49fd9808b8";

export default node;

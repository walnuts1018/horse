/**
 * @generated SignedSource<<42450391d4781e7f7757da11f9232278>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type ArchiveRecurringExpenseMutation$variables = {
  archived: boolean;
  baseRevision: number;
  expenseId: string;
};
export type ArchiveRecurringExpenseMutation$data = {
  readonly archiveRecurringExpense: {
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
export type ArchiveRecurringExpenseMutation = {
  response: ArchiveRecurringExpenseMutation$data;
  variables: ArchiveRecurringExpenseMutation$variables;
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
  "name": "expenseId"
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
        "name": "expenseId",
        "variableName": "expenseId"
      }
    ],
    "concreteType": null,
    "kind": "LinkedField",
    "name": "archiveRecurringExpense",
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
            "concreteType": "RecurringExpense",
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
        "type": "ArchiveRecurringExpenseSuccess",
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
    "name": "ArchiveRecurringExpenseMutation",
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
    "name": "ArchiveRecurringExpenseMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "7bee09069b5671b12cdcf7240bd3d749",
    "id": null,
    "metadata": {},
    "name": "ArchiveRecurringExpenseMutation",
    "operationKind": "mutation",
    "text": "mutation ArchiveRecurringExpenseMutation(\n  $expenseId: ID!\n  $baseRevision: Int!\n  $archived: Boolean!\n) {\n  archiveRecurringExpense(expenseId: $expenseId, baseRevision: $baseRevision, archived: $archived) {\n    __typename\n    ... on ArchiveRecurringExpenseSuccess {\n      result {\n        id\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "1889dcfdbaa4fdc29bb8912aa2919308";

export default node;

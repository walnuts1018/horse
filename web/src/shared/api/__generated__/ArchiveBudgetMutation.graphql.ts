/**
 * @generated SignedSource<<a5aced483d592a85a5550cb7b3f03d0a>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type ArchiveBudgetInput = {
  archived: boolean;
  baseRevision: number;
  budgetId: string;
};
export type ArchiveBudgetMutation$variables = {
  input: ArchiveBudgetInput;
};
export type ArchiveBudgetMutation$data = {
  readonly archiveBudget: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly archived: boolean;
      readonly budgetId: string;
      readonly revision: number;
    };
  };
};
export type ArchiveBudgetMutation = {
  response: ArchiveBudgetMutation$data;
  variables: ArchiveBudgetMutation$variables;
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
            "concreteType": "BudgetSettings",
            "kind": "LinkedField",
            "name": "result",
            "plural": false,
            "selections": [
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "budgetId",
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
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "ArchiveBudgetMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "ArchiveBudgetMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "cb6bb538430771e3a4ce371ce48847db",
    "id": null,
    "metadata": {},
    "name": "ArchiveBudgetMutation",
    "operationKind": "mutation",
    "text": "mutation ArchiveBudgetMutation(\n  $input: ArchiveBudgetInput!\n) {\n  archiveBudget(input: $input) {\n    __typename\n    ... on ArchiveBudgetSuccess {\n      result {\n        budgetId\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "234dddfc2a449358ec362ca1a4558658";

export default node;

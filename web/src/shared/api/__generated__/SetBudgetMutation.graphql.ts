/**
 * @generated SignedSource<<ff9744fdba9b8d09e14c65aa3a3c7056>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SetBudgetInput = {
  amountYen: any;
  baseRevision?: number | null | undefined;
  budgetId?: string | null | undefined;
  categoryId?: string | null | undefined;
  endsOn?: string | null | undefined;
  householdId: string;
  name: string;
  startsOn: string;
};
export type SetBudgetMutation$variables = {
  input: SetBudgetInput;
};
export type SetBudgetMutation$data = {
  readonly setBudget: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly actualYen: any;
      readonly amountYen: any;
      readonly archived: boolean;
      readonly categoryId: string | null | undefined;
      readonly endsOn: string | null | undefined;
      readonly id: string;
      readonly name: string;
      readonly remainingYen: any;
      readonly revision: number;
      readonly startsOn: string;
    };
  };
};
export type SetBudgetMutation = {
  response: SetBudgetMutation$data;
  variables: SetBudgetMutation$variables;
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
    "name": "setBudget",
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
                "name": "categoryId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "name",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "amountYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "actualYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "remainingYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "startsOn",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "endsOn",
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
        "type": "SetBudgetSuccess",
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
    "name": "SetBudgetMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetBudgetMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "d9c339eab74d20c63d4026c244708403",
    "id": null,
    "metadata": {},
    "name": "SetBudgetMutation",
    "operationKind": "mutation",
    "text": "mutation SetBudgetMutation(\n  $input: SetBudgetInput!\n) {\n  setBudget(input: $input) {\n    __typename\n    ... on SetBudgetSuccess {\n      result {\n        id\n        categoryId\n        name\n        amountYen\n        actualYen\n        remainingYen\n        startsOn\n        endsOn\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "811abbf4a0750cf7be2d822b3462a29a";

export default node;

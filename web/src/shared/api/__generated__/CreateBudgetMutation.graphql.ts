/**
 * @generated SignedSource<<5d9ec176ef8495f82580b54cf931e376>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type CreateBudgetInput = {
  amountYen: any;
  categoryId?: string | null | undefined;
  endsOn?: string | null | undefined;
  householdId: string;
  name: string;
  startsOn: string;
};
export type CreateBudgetMutation$variables = {
  input: CreateBudgetInput;
};
export type CreateBudgetMutation$data = {
  readonly createBudget: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly amountYen: any;
      readonly archived: boolean;
      readonly budgetId: string;
      readonly categoryId: string | null | undefined;
      readonly endsOn: string | null | undefined;
      readonly name: string;
      readonly revision: number;
      readonly startsOn: string;
    };
  };
};
export type CreateBudgetMutation = {
  response: CreateBudgetMutation$data;
  variables: CreateBudgetMutation$variables;
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
    "name": "createBudget",
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
        "type": "CreateBudgetSuccess",
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
    "name": "CreateBudgetMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateBudgetMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "86fab5665e87c79364e4ff79e66c2790",
    "id": null,
    "metadata": {},
    "name": "CreateBudgetMutation",
    "operationKind": "mutation",
    "text": "mutation CreateBudgetMutation(\n  $input: CreateBudgetInput!\n) {\n  createBudget(input: $input) {\n    __typename\n    ... on CreateBudgetSuccess {\n      result {\n        budgetId\n        categoryId\n        name\n        amountYen\n        startsOn\n        endsOn\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "12cf39176999e17de8f263ab4ff58a00";

export default node;

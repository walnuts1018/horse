/**
 * @generated SignedSource<<430f7d02c86bdce8fa25775aa04ebc66>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SetRecurringExpenseInput = {
  amountYen: any;
  baseRevision?: number | null | undefined;
  cadence: string;
  categoryId?: string | null | undefined;
  endsOn?: string | null | undefined;
  expenseId?: string | null | undefined;
  householdId: string;
  merchant: string;
  name: string;
  nextDueOn: string;
  paymentAccountId?: string | null | undefined;
  startsOn: string;
};
export type SetRecurringExpenseMutation$variables = {
  input: SetRecurringExpenseInput;
};
export type SetRecurringExpenseMutation$data = {
  readonly setRecurringExpense: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly amountYen: any;
      readonly archived: boolean;
      readonly cadence: string;
      readonly categoryId: string | null | undefined;
      readonly endsOn: string | null | undefined;
      readonly id: string;
      readonly merchant: string;
      readonly name: string;
      readonly nextDueOn: string;
      readonly paymentAccountId: string | null | undefined;
      readonly revision: number;
      readonly startsOn: string;
    };
  };
};
export type SetRecurringExpenseMutation = {
  response: SetRecurringExpenseMutation$data;
  variables: SetRecurringExpenseMutation$variables;
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
    "name": "setRecurringExpense",
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
                "name": "name",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "merchant",
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
                "name": "categoryId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "paymentAccountId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "cadence",
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
                "name": "nextDueOn",
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
        "type": "SetRecurringExpenseSuccess",
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
    "name": "SetRecurringExpenseMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetRecurringExpenseMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "4b33889adbecb1cb487a13d2b06c652b",
    "id": null,
    "metadata": {},
    "name": "SetRecurringExpenseMutation",
    "operationKind": "mutation",
    "text": "mutation SetRecurringExpenseMutation(\n  $input: SetRecurringExpenseInput!\n) {\n  setRecurringExpense(input: $input) {\n    __typename\n    ... on SetRecurringExpenseSuccess {\n      result {\n        id\n        name\n        merchant\n        amountYen\n        categoryId\n        paymentAccountId\n        cadence\n        startsOn\n        nextDueOn\n        endsOn\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "cf4e527fc3ec1d81e3e3827882b8a9a7";

export default node;

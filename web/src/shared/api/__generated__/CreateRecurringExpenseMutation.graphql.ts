/**
 * @generated SignedSource<<88f2540837c3d34e40419ea9ad08063e>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type RecurringExpenseCadence = "MONTHLY" | "WEEKLY" | "YEARLY" | "%future added value";
export type CreateRecurringExpenseInput = {
  amountYen: any;
  cadence: RecurringExpenseCadence;
  categoryId?: string | null | undefined;
  endsOn?: string | null | undefined;
  householdId: string;
  merchant: string;
  name: string;
  nextDueOn: string;
  paymentAccountId?: string | null | undefined;
  startsOn: string;
};
export type CreateRecurringExpenseMutation$variables = {
  input: CreateRecurringExpenseInput;
};
export type CreateRecurringExpenseMutation$data = {
  readonly createRecurringExpense: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly amountYen: any;
      readonly archived: boolean;
      readonly cadence: RecurringExpenseCadence;
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
export type CreateRecurringExpenseMutation = {
  response: CreateRecurringExpenseMutation$data;
  variables: CreateRecurringExpenseMutation$variables;
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
    "name": "createRecurringExpense",
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
        "type": "CreateRecurringExpenseSuccess",
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
    "name": "CreateRecurringExpenseMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateRecurringExpenseMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "8a79129b763e526af306c53ce6ba67d3",
    "id": null,
    "metadata": {},
    "name": "CreateRecurringExpenseMutation",
    "operationKind": "mutation",
    "text": "mutation CreateRecurringExpenseMutation(\n  $input: CreateRecurringExpenseInput!\n) {\n  createRecurringExpense(input: $input) {\n    __typename\n    ... on CreateRecurringExpenseSuccess {\n      result {\n        id\n        name\n        merchant\n        amountYen\n        categoryId\n        paymentAccountId\n        cadence\n        startsOn\n        nextDueOn\n        endsOn\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "81832e1aab5bac20ee002c4be75254c9";

export default node;

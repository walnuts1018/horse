/**
 * @generated SignedSource<<3455caa6f9427b00742fe55ef29aeded>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
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
    "concreteType": "RecurringExpense",
    "kind": "LinkedField",
    "name": "setRecurringExpense",
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
    "cacheID": "51a11d754b3a4522b4c4e0bb74cd6ae1",
    "id": null,
    "metadata": {},
    "name": "SetRecurringExpenseMutation",
    "operationKind": "mutation",
    "text": "mutation SetRecurringExpenseMutation(\n  $input: SetRecurringExpenseInput!\n) {\n  setRecurringExpense(input: $input) {\n    id\n    name\n    merchant\n    amountYen\n    categoryId\n    paymentAccountId\n    cadence\n    startsOn\n    nextDueOn\n    endsOn\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "3acfcdce2f50a0c5db70ee5b9c462088";

export default node;

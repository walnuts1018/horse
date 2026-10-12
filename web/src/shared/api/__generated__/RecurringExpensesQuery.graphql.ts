/**
 * @generated SignedSource<<7d06f279570024b5294841deaed88816>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type RecurringExpensesQuery$variables = {
  householdId: string;
  includeArchived?: boolean | null | undefined;
};
export type RecurringExpensesQuery$data = {
  readonly recurringExpenses: ReadonlyArray<{
    readonly amountYen: any;
    readonly archived: boolean;
    readonly cadence: string;
    readonly categoryId: string | null | undefined;
    readonly currency: string;
    readonly endsOn: string | null | undefined;
    readonly householdId: string;
    readonly id: string;
    readonly merchant: string;
    readonly name: string;
    readonly nextDueOn: string;
    readonly paymentAccountId: string | null | undefined;
    readonly revision: number;
    readonly startsOn: string;
  }>;
};
export type RecurringExpensesQuery = {
  response: RecurringExpensesQuery$data;
  variables: RecurringExpensesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "includeArchived"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      },
      {
        "kind": "Variable",
        "name": "includeArchived",
        "variableName": "includeArchived"
      }
    ],
    "concreteType": "RecurringExpense",
    "kind": "LinkedField",
    "name": "recurringExpenses",
    "plural": true,
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
        "name": "householdId",
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
        "name": "currency",
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
    "name": "RecurringExpensesQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecurringExpensesQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "ca30887f3041e0ffec12ffccef24acd4",
    "id": null,
    "metadata": {},
    "name": "RecurringExpensesQuery",
    "operationKind": "query",
    "text": "query RecurringExpensesQuery(\n  $householdId: ID!\n  $includeArchived: Boolean\n) {\n  recurringExpenses(householdId: $householdId, includeArchived: $includeArchived) {\n    id\n    householdId\n    name\n    merchant\n    amountYen\n    currency\n    categoryId\n    paymentAccountId\n    cadence\n    startsOn\n    nextDueOn\n    endsOn\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "db2ba9088f10a140ccef0db7ce33c286";

export default node;

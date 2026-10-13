/**
 * @generated SignedSource<<1427edb1ed325fd030b0cd9e29e6e17e>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MonthlyCategorySpendingQuery$variables = {
  from: string;
  householdId: string;
  to: string;
};
export type MonthlyCategorySpendingQuery$data = {
  readonly monthlyCategorySpending: ReadonlyArray<{
    readonly amountYen: any;
    readonly categoryId: string | null | undefined;
    readonly categoryName: string | null | undefined;
    readonly period: string;
  }>;
};
export type MonthlyCategorySpendingQuery = {
  response: MonthlyCategorySpendingQuery$data;
  variables: MonthlyCategorySpendingQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "from"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v3 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "from",
        "variableName": "from"
      },
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      },
      {
        "kind": "Variable",
        "name": "to",
        "variableName": "to"
      }
    ],
    "concreteType": "MonthlyCategorySpending",
    "kind": "LinkedField",
    "name": "monthlyCategorySpending",
    "plural": true,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "period",
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
        "name": "categoryName",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "amountYen",
        "storageKey": null
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
    "name": "MonthlyCategorySpendingQuery",
    "selections": (v3/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "MonthlyCategorySpendingQuery",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "66e07d8d67b5330b602843eba6864172",
    "id": null,
    "metadata": {},
    "name": "MonthlyCategorySpendingQuery",
    "operationKind": "query",
    "text": "query MonthlyCategorySpendingQuery(\n  $householdId: ID!\n  $from: YearMonth!\n  $to: YearMonth!\n) {\n  monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {\n    period\n    categoryId\n    categoryName\n    amountYen\n  }\n}\n"
  }
};
})();

(node as any).hash = "af6708650e0f81d34cbcc5cf2e8b1be7";

export default node;

/**
 * @generated SignedSource<<c9c42c5213921c5288e3241da300fbb7>>
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
    "cacheID": "c0f6006c4be35d42432bc3c4d5c92407",
    "id": null,
    "metadata": {},
    "name": "MonthlyCategorySpendingQuery",
    "operationKind": "query",
    "text": "query MonthlyCategorySpendingQuery(\n  $householdId: ID!\n  $from: String!\n  $to: String!\n) {\n  monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {\n    period\n    categoryId\n    categoryName\n    amountYen\n  }\n}\n"
  }
};
})();

(node as any).hash = "d3ca070c8be6943884bdda40e3f9b2d7";

export default node;

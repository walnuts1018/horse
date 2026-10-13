/**
 * @generated SignedSource<<24428647b674232100a9ef97054d7cf1>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MonthlyMemberSpendingQuery$variables = {
  from: string;
  householdId: string;
  to: string;
};
export type MonthlyMemberSpendingQuery$data = {
  readonly monthlyMemberSpending: {
    readonly hasUnattributedPurchases: boolean;
    readonly items: ReadonlyArray<{
      readonly amountYen: any;
      readonly memberId: string;
      readonly memberName: string;
      readonly period: string;
    }>;
  };
};
export type MonthlyMemberSpendingQuery = {
  response: MonthlyMemberSpendingQuery$data;
  variables: MonthlyMemberSpendingQuery$variables;
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
    "concreteType": "MonthlyMemberSpendingReport",
    "kind": "LinkedField",
    "name": "monthlyMemberSpending",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "MonthlyMemberSpending",
        "kind": "LinkedField",
        "name": "items",
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
            "name": "memberId",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "memberName",
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
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "hasUnattributedPurchases",
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
    "name": "MonthlyMemberSpendingQuery",
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
    "name": "MonthlyMemberSpendingQuery",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "d2fc6242bcd783e611837e5dfdfc5f34",
    "id": null,
    "metadata": {},
    "name": "MonthlyMemberSpendingQuery",
    "operationKind": "query",
    "text": "query MonthlyMemberSpendingQuery(\n  $householdId: ID!\n  $from: YearMonth!\n  $to: YearMonth!\n) {\n  monthlyMemberSpending(householdId: $householdId, from: $from, to: $to) {\n    items {\n      period\n      memberId\n      memberName\n      amountYen\n    }\n    hasUnattributedPurchases\n  }\n}\n"
  }
};
})();

(node as any).hash = "e5dfec6402b3b3737595209def994d7c";

export default node;

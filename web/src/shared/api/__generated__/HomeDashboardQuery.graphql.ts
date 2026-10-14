/**
 * @generated SignedSource<<155694f97c4c1766eac5058ef5d54a8d>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HomeDashboardQuery$variables = {
  from: string;
  householdId: string;
  period: string;
  to: string;
};
export type HomeDashboardQuery$data = {
  readonly homeSummary: {
    readonly dailySpending: ReadonlyArray<{
      readonly amountYen: any;
      readonly date: string;
    }>;
    readonly monthlyItemCount: any;
  };
  readonly monthlyCategorySpending: ReadonlyArray<{
    readonly amountYen: any;
    readonly period: string;
  }>;
  readonly settlement: {
    readonly __typename: string;
    readonly lines: ReadonlyArray<{
      readonly amountDueYen: any;
      readonly memberId: string;
      readonly memberName: string;
    }>;
  };
};
export type HomeDashboardQuery = {
  response: HomeDashboardQuery$data;
  variables: HomeDashboardQuery$variables;
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
  "name": "period"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v4 = {
  "kind": "Variable",
  "name": "householdId",
  "variableName": "householdId"
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": [
    (v4/*:: as any*/)
  ],
  "concreteType": "HomeSummary",
  "kind": "LinkedField",
  "name": "homeSummary",
  "plural": false,
  "selections": [
    {
      "alias": null,
      "args": null,
      "concreteType": "HomeDailySpending",
      "kind": "LinkedField",
      "name": "dailySpending",
      "plural": true,
      "selections": [
        {
          "alias": null,
          "args": null,
          "kind": "ScalarField",
          "name": "date",
          "storageKey": null
        },
        (v5/*:: as any*/)
      ],
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "monthlyItemCount",
      "storageKey": null
    }
  ],
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": [
    {
      "kind": "Variable",
      "name": "from",
      "variableName": "from"
    },
    (v4/*:: as any*/),
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
    (v5/*:: as any*/)
  ],
  "storageKey": null
},
v8 = [
  (v4/*:: as any*/),
  {
    "kind": "Variable",
    "name": "period",
    "variableName": "period"
  }
],
v9 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v10 = {
  "alias": null,
  "args": null,
  "concreteType": "SettlementLine",
  "kind": "LinkedField",
  "name": "lines",
  "plural": true,
  "selections": [
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
      "name": "amountDueYen",
      "storageKey": null
    }
  ],
  "storageKey": null
};
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v3/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "HomeDashboardQuery",
    "selections": [
      (v6/*:: as any*/),
      (v7/*:: as any*/),
      {
        "alias": null,
        "args": (v8/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "settlement",
        "plural": false,
        "selections": [
          (v9/*:: as any*/),
          (v10/*:: as any*/)
        ],
        "storageKey": null
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/),
      (v3/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "HomeDashboardQuery",
    "selections": [
      (v6/*:: as any*/),
      (v7/*:: as any*/),
      {
        "alias": null,
        "args": (v8/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "settlement",
        "plural": false,
        "selections": [
          (v9/*:: as any*/),
          (v10/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "id",
            "storageKey": null
          }
        ],
        "storageKey": null
      }
    ]
  },
  "params": {
    "cacheID": "141bbc51e242796d17ed6eaf22fe9c92",
    "id": null,
    "metadata": {},
    "name": "HomeDashboardQuery",
    "operationKind": "query",
    "text": "query HomeDashboardQuery(\n  $householdId: ID!\n  $from: YearMonth!\n  $to: YearMonth!\n  $period: YearMonth!\n) {\n  homeSummary(householdId: $householdId) {\n    dailySpending {\n      date\n      amountYen\n    }\n    monthlyItemCount\n  }\n  monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {\n    period\n    amountYen\n  }\n  settlement(householdId: $householdId, period: $period) {\n    __typename\n    lines {\n      memberId\n      memberName\n      amountDueYen\n    }\n    id\n  }\n}\n"
  }
};
})();

(node as any).hash = "832ef98f8c4b6689744dae36abf5ffa9";

export default node;

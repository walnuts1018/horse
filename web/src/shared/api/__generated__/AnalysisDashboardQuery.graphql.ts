/**
 * @generated SignedSource<<f0ff7d1a898e14a63be5ba86a2d34eaa>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type AnalysisDashboardQuery$variables = {
  from: string;
  householdId: string;
  to: string;
};
export type AnalysisDashboardQuery$data = {
  readonly budgetPeriods: ReadonlyArray<{
    readonly budgets: ReadonlyArray<{
      readonly actualYen: any;
      readonly amountYen: any;
      readonly archived: boolean;
      readonly budgetId: string;
      readonly categoryId: string | null | undefined;
      readonly endsOn: string | null | undefined;
      readonly householdId: string;
      readonly name: string;
      readonly remainingYen: any;
      readonly revision: number;
      readonly startsOn: string;
    }>;
    readonly period: string;
  }>;
  readonly monthlyCategorySpending: ReadonlyArray<{
    readonly amountYen: any;
    readonly categoryId: string | null | undefined;
    readonly categoryName: string | null | undefined;
    readonly period: string;
  }>;
  readonly monthlyMemberSpending: {
    readonly hasUnattributedPurchases: boolean;
    readonly items: ReadonlyArray<{
      readonly amountYen: any;
      readonly memberId: string;
      readonly memberName: string;
      readonly period: string;
    }>;
  };
  readonly purchaseAnalytics: ReadonlyArray<{
    readonly amountYen: any;
    readonly count: number;
    readonly dimension: string;
    readonly dimensionId: string;
    readonly name: string;
    readonly period: string;
  }>;
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisRecurringExpensesFragment">;
};
export type AnalysisDashboardQuery = {
  response: AnalysisDashboardQuery$data;
  variables: AnalysisDashboardQuery$variables;
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
v3 = {
  "kind": "Variable",
  "name": "householdId",
  "variableName": "householdId"
},
v4 = [
  {
    "kind": "Variable",
    "name": "from",
    "variableName": "from"
  },
  (v3/*:: as any*/),
  {
    "kind": "Variable",
    "name": "to",
    "variableName": "to"
  }
],
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "period",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "categoryId",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": (v4/*:: as any*/),
  "concreteType": "MonthlyCategorySpending",
  "kind": "LinkedField",
  "name": "monthlyCategorySpending",
  "plural": true,
  "selections": [
    (v5/*:: as any*/),
    (v6/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "categoryName",
      "storageKey": null
    },
    (v7/*:: as any*/)
  ],
  "storageKey": null
},
v9 = {
  "alias": null,
  "args": (v4/*:: as any*/),
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
        (v5/*:: as any*/),
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
        (v7/*:: as any*/)
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
},
v10 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v11 = {
  "alias": null,
  "args": (v4/*:: as any*/),
  "concreteType": "PurchaseAnalyticsEntry",
  "kind": "LinkedField",
  "name": "purchaseAnalytics",
  "plural": true,
  "selections": [
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "dimension",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "dimensionId",
      "storageKey": null
    },
    (v10/*:: as any*/),
    (v5/*:: as any*/),
    (v7/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "count",
      "storageKey": null
    }
  ],
  "storageKey": null
},
v12 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v13 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "startsOn",
  "storageKey": null
},
v14 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "endsOn",
  "storageKey": null
},
v15 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "archived",
  "storageKey": null
},
v16 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v17 = {
  "alias": null,
  "args": (v4/*:: as any*/),
  "concreteType": "BudgetPeriod",
  "kind": "LinkedField",
  "name": "budgetPeriods",
  "plural": true,
  "selections": [
    (v5/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "concreteType": "BudgetPeriodEntry",
      "kind": "LinkedField",
      "name": "budgets",
      "plural": true,
      "selections": [
        {
          "alias": null,
          "args": null,
          "kind": "ScalarField",
          "name": "budgetId",
          "storageKey": null
        },
        (v12/*:: as any*/),
        (v6/*:: as any*/),
        (v10/*:: as any*/),
        (v7/*:: as any*/),
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
        (v13/*:: as any*/),
        (v14/*:: as any*/),
        (v15/*:: as any*/),
        (v16/*:: as any*/)
      ],
      "storageKey": null
    }
  ],
  "storageKey": null
},
v18 = [
  {
    "kind": "Literal",
    "name": "first",
    "value": 100
  },
  (v3/*:: as any*/),
  {
    "kind": "Literal",
    "name": "includeArchived",
    "value": false
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
    "name": "AnalysisDashboardQuery",
    "selections": [
      (v8/*:: as any*/),
      (v9/*:: as any*/),
      (v11/*:: as any*/),
      (v17/*:: as any*/),
      {
        "args": [
          (v3/*:: as any*/)
        ],
        "kind": "FragmentSpread",
        "name": "AnalysisRecurringExpensesFragment"
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
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "AnalysisDashboardQuery",
    "selections": [
      (v8/*:: as any*/),
      (v9/*:: as any*/),
      (v11/*:: as any*/),
      (v17/*:: as any*/),
      {
        "alias": null,
        "args": (v18/*:: as any*/),
        "concreteType": "RecurringExpenseConnection",
        "kind": "LinkedField",
        "name": "recurringExpenses",
        "plural": false,
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "RecurringExpenseEdge",
            "kind": "LinkedField",
            "name": "edges",
            "plural": true,
            "selections": [
              {
                "alias": null,
                "args": null,
                "concreteType": "RecurringExpense",
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
                "selections": [
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "id",
                    "storageKey": null
                  },
                  (v12/*:: as any*/),
                  (v10/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "merchant",
                    "storageKey": null
                  },
                  (v7/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "currency",
                    "storageKey": null
                  },
                  (v6/*:: as any*/),
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
                  (v13/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "nextDueOn",
                    "storageKey": null
                  },
                  (v14/*:: as any*/),
                  (v15/*:: as any*/),
                  (v16/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "__typename",
                    "storageKey": null
                  }
                ],
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "cursor",
                "storageKey": null
              }
            ],
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "PageInfo",
            "kind": "LinkedField",
            "name": "pageInfo",
            "plural": false,
            "selections": [
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "endCursor",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "hasNextPage",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": (v18/*:: as any*/),
        "filters": [
          "householdId",
          "includeArchived"
        ],
        "handle": "connection",
        "key": "AnalysisRecurringExpensesFragment_recurringExpenses",
        "kind": "LinkedHandle",
        "name": "recurringExpenses"
      }
    ]
  },
  "params": {
    "cacheID": "d32decc913877dac12c8187d906ffdf8",
    "id": null,
    "metadata": {},
    "name": "AnalysisDashboardQuery",
    "operationKind": "query",
    "text": "query AnalysisDashboardQuery(\n  $householdId: ID!\n  $from: YearMonth!\n  $to: YearMonth!\n) {\n  monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {\n    period\n    categoryId\n    categoryName\n    amountYen\n  }\n  monthlyMemberSpending(householdId: $householdId, from: $from, to: $to) {\n    items {\n      period\n      memberId\n      memberName\n      amountYen\n    }\n    hasUnattributedPurchases\n  }\n  purchaseAnalytics(householdId: $householdId, from: $from, to: $to) {\n    dimension\n    dimensionId\n    name\n    period\n    amountYen\n    count\n  }\n  budgetPeriods(householdId: $householdId, from: $from, to: $to) {\n    period\n    budgets {\n      budgetId\n      householdId\n      categoryId\n      name\n      amountYen\n      actualYen\n      remainingYen\n      startsOn\n      endsOn\n      archived\n      revision\n    }\n  }\n  ...AnalysisRecurringExpensesFragment_2BD5SD\n}\n\nfragment AnalysisRecurringExpensesFragment_2BD5SD on Query {\n  recurringExpenses(householdId: $householdId, includeArchived: false, first: 100) {\n    edges {\n      node {\n        id\n        householdId\n        name\n        merchant\n        amountYen\n        currency\n        categoryId\n        paymentAccountId\n        cadence\n        startsOn\n        nextDueOn\n        endsOn\n        archived\n        revision\n        __typename\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "911e2afa34107909f79033973be7683a";

export default node;

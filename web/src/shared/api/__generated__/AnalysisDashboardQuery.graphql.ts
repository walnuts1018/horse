/**
 * @generated SignedSource<<d99134685ca1fbf66a1bb22e6cbea9a4>>
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
  period: string;
  to: string;
};
export type AnalysisDashboardQuery$data = {
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
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisBudgetsFragment" | "AnalysisRecurringExpensesFragment">;
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
v5 = [
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
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "period",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "categoryId",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v9 = {
  "alias": null,
  "args": (v5/*:: as any*/),
  "concreteType": "MonthlyCategorySpending",
  "kind": "LinkedField",
  "name": "monthlyCategorySpending",
  "plural": true,
  "selections": [
    (v6/*:: as any*/),
    (v7/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "categoryName",
      "storageKey": null
    },
    (v8/*:: as any*/)
  ],
  "storageKey": null
},
v10 = {
  "alias": null,
  "args": (v5/*:: as any*/),
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
        (v6/*:: as any*/),
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
        (v8/*:: as any*/)
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
v11 = {
  "kind": "Variable",
  "name": "period",
  "variableName": "period"
},
v12 = {
  "kind": "Literal",
  "name": "first",
  "value": 100
},
v13 = [
  (v12/*:: as any*/),
  (v4/*:: as any*/),
  (v11/*:: as any*/)
],
v14 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v15 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v16 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v17 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "startsOn",
  "storageKey": null
},
v18 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "endsOn",
  "storageKey": null
},
v19 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "archived",
  "storageKey": null
},
v20 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v21 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v22 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "cursor",
  "storageKey": null
},
v23 = {
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
},
v24 = [
  (v12/*:: as any*/),
  (v4/*:: as any*/),
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
      (v2/*:: as any*/),
      (v3/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "AnalysisDashboardQuery",
    "selections": [
      (v9/*:: as any*/),
      (v10/*:: as any*/),
      {
        "args": [
          (v4/*:: as any*/),
          (v11/*:: as any*/)
        ],
        "kind": "FragmentSpread",
        "name": "AnalysisBudgetsFragment"
      },
      {
        "args": [
          (v4/*:: as any*/)
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
      (v3/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "AnalysisDashboardQuery",
    "selections": [
      (v9/*:: as any*/),
      (v10/*:: as any*/),
      {
        "alias": null,
        "args": (v13/*:: as any*/),
        "concreteType": "BudgetConnection",
        "kind": "LinkedField",
        "name": "budgets",
        "plural": false,
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "BudgetEdge",
            "kind": "LinkedField",
            "name": "edges",
            "plural": true,
            "selections": [
              {
                "alias": null,
                "args": null,
                "concreteType": "Budget",
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
                "selections": [
                  (v14/*:: as any*/),
                  (v15/*:: as any*/),
                  (v7/*:: as any*/),
                  (v16/*:: as any*/),
                  (v8/*:: as any*/),
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
                  (v17/*:: as any*/),
                  (v18/*:: as any*/),
                  (v19/*:: as any*/),
                  (v20/*:: as any*/),
                  (v21/*:: as any*/)
                ],
                "storageKey": null
              },
              (v22/*:: as any*/)
            ],
            "storageKey": null
          },
          (v23/*:: as any*/)
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": (v13/*:: as any*/),
        "filters": [
          "householdId",
          "period"
        ],
        "handle": "connection",
        "key": "AnalysisBudgetsFragment_budgets",
        "kind": "LinkedHandle",
        "name": "budgets"
      },
      {
        "alias": null,
        "args": (v24/*:: as any*/),
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
                  (v14/*:: as any*/),
                  (v15/*:: as any*/),
                  (v16/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "merchant",
                    "storageKey": null
                  },
                  (v8/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "currency",
                    "storageKey": null
                  },
                  (v7/*:: as any*/),
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
                  (v17/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "nextDueOn",
                    "storageKey": null
                  },
                  (v18/*:: as any*/),
                  (v19/*:: as any*/),
                  (v20/*:: as any*/),
                  (v21/*:: as any*/)
                ],
                "storageKey": null
              },
              (v22/*:: as any*/)
            ],
            "storageKey": null
          },
          (v23/*:: as any*/)
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": (v24/*:: as any*/),
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
    "cacheID": "a3e29f4e9a23cd725d49fd34b8c03c47",
    "id": null,
    "metadata": {},
    "name": "AnalysisDashboardQuery",
    "operationKind": "query",
    "text": "query AnalysisDashboardQuery(\n  $householdId: ID!\n  $from: YearMonth!\n  $to: YearMonth!\n  $period: YearMonth!\n) {\n  monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {\n    period\n    categoryId\n    categoryName\n    amountYen\n  }\n  monthlyMemberSpending(householdId: $householdId, from: $from, to: $to) {\n    items {\n      period\n      memberId\n      memberName\n      amountYen\n    }\n    hasUnattributedPurchases\n  }\n  ...AnalysisBudgetsFragment_50vXu\n  ...AnalysisRecurringExpensesFragment_2BD5SD\n}\n\nfragment AnalysisBudgetsFragment_50vXu on Query {\n  budgets(householdId: $householdId, period: $period, first: 100) {\n    edges {\n      node {\n        id\n        householdId\n        categoryId\n        name\n        amountYen\n        actualYen\n        remainingYen\n        startsOn\n        endsOn\n        archived\n        revision\n        __typename\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n\nfragment AnalysisRecurringExpensesFragment_2BD5SD on Query {\n  recurringExpenses(householdId: $householdId, includeArchived: false, first: 100) {\n    edges {\n      node {\n        id\n        householdId\n        name\n        merchant\n        amountYen\n        currency\n        categoryId\n        paymentAccountId\n        cadence\n        startsOn\n        nextDueOn\n        endsOn\n        archived\n        revision\n        __typename\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "fa4751dc04c35986e77a6847a3379dfc";

export default node;

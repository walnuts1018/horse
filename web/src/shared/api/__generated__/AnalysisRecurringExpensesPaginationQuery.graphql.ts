/**
 * @generated SignedSource<<6fd6c0c96d84b8a5a9d3f296c468e5b9>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type AnalysisRecurringExpensesPaginationQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type AnalysisRecurringExpensesPaginationQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisRecurringExpensesFragment">;
};
export type AnalysisRecurringExpensesPaginationQuery = {
  response: AnalysisRecurringExpensesPaginationQuery$data;
  variables: AnalysisRecurringExpensesPaginationQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "after"
  },
  {
    "defaultValue": 100,
    "kind": "LocalArgument",
    "name": "first"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  }
],
v1 = {
  "kind": "Variable",
  "name": "after",
  "variableName": "after"
},
v2 = {
  "kind": "Variable",
  "name": "first",
  "variableName": "first"
},
v3 = {
  "kind": "Variable",
  "name": "householdId",
  "variableName": "householdId"
},
v4 = [
  (v1/*:: as any*/),
  (v2/*:: as any*/),
  (v3/*:: as any*/),
  {
    "kind": "Literal",
    "name": "includeArchived",
    "value": false
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "AnalysisRecurringExpensesPaginationQuery",
    "selections": [
      {
        "args": [
          (v1/*:: as any*/),
          (v2/*:: as any*/),
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
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AnalysisRecurringExpensesPaginationQuery",
    "selections": [
      {
        "alias": null,
        "args": (v4/*:: as any*/),
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
                  },
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
        "args": (v4/*:: as any*/),
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
    "cacheID": "096044455079c153e4ab88d227700b00",
    "id": null,
    "metadata": {},
    "name": "AnalysisRecurringExpensesPaginationQuery",
    "operationKind": "query",
    "text": "query AnalysisRecurringExpensesPaginationQuery(\n  $after: String\n  $first: Int = 100\n  $householdId: ID!\n) {\n  ...AnalysisRecurringExpensesFragment_1x5y0e\n}\n\nfragment AnalysisRecurringExpensesFragment_1x5y0e on Query {\n  recurringExpenses(householdId: $householdId, includeArchived: false, first: $first, after: $after) {\n    edges {\n      node {\n        id\n        householdId\n        name\n        merchant\n        amountYen\n        currency\n        categoryId\n        paymentAccountId\n        cadence\n        startsOn\n        nextDueOn\n        endsOn\n        archived\n        revision\n        __typename\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "6f53f1675c1140a7653c720381d5082f";

export default node;

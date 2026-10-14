/**
 * @generated SignedSource<<c67426ccfff4d6fcb9ab43d22ab09111>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type AnalysisBudgetsPaginationQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
  period: string;
};
export type AnalysisBudgetsPaginationQuery$data = {
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisBudgetsFragment">;
};
export type AnalysisBudgetsPaginationQuery = {
  response: AnalysisBudgetsPaginationQuery$data;
  variables: AnalysisBudgetsPaginationQuery$variables;
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
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "period"
  }
],
v1 = [
  {
    "kind": "Variable",
    "name": "after",
    "variableName": "after"
  },
  {
    "kind": "Variable",
    "name": "first",
    "variableName": "first"
  },
  {
    "kind": "Variable",
    "name": "householdId",
    "variableName": "householdId"
  },
  {
    "kind": "Variable",
    "name": "period",
    "variableName": "period"
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "AnalysisBudgetsPaginationQuery",
    "selections": [
      {
        "args": (v1/*:: as any*/),
        "kind": "FragmentSpread",
        "name": "AnalysisBudgetsFragment"
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AnalysisBudgetsPaginationQuery",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
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
                    "name": "categoryId",
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
                    "name": "amountYen",
                    "storageKey": null
                  },
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
        "args": (v1/*:: as any*/),
        "filters": [
          "householdId",
          "period"
        ],
        "handle": "connection",
        "key": "AnalysisBudgetsFragment_budgets",
        "kind": "LinkedHandle",
        "name": "budgets"
      }
    ]
  },
  "params": {
    "cacheID": "4be2a2fcd38b898ec2d3bd7835f9f62c",
    "id": null,
    "metadata": {},
    "name": "AnalysisBudgetsPaginationQuery",
    "operationKind": "query",
    "text": "query AnalysisBudgetsPaginationQuery(\n  $after: String\n  $first: Int = 100\n  $householdId: ID!\n  $period: YearMonth!\n) {\n  ...AnalysisBudgetsFragment_c7bGC\n}\n\nfragment AnalysisBudgetsFragment_c7bGC on Query {\n  budgets(householdId: $householdId, period: $period, first: $first, after: $after) {\n    edges {\n      node {\n        id\n        householdId\n        categoryId\n        name\n        amountYen\n        actualYen\n        remainingYen\n        startsOn\n        endsOn\n        archived\n        revision\n        __typename\n      }\n      cursor\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "332aefca1f45590ff8b1a5761880f1f2";

export default node;

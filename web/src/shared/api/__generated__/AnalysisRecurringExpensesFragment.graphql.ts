/**
 * @generated SignedSource<<65667c12e2b16d8b86c64ea1e0e911bd>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
export type RecurringExpenseCadence = "MONTHLY" | "WEEKLY" | "YEARLY" | "%future added value";
import type { FragmentRefs } from "relay-runtime";
export type AnalysisRecurringExpensesFragment$data = {
  readonly recurringExpenses: {
    readonly edges: ReadonlyArray<{
      readonly node: {
        readonly amountYen: any;
        readonly archived: boolean;
        readonly cadence: RecurringExpenseCadence;
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
      };
    }>;
  };
  readonly " $fragmentType": "AnalysisRecurringExpensesFragment";
};
export type AnalysisRecurringExpensesFragment$key = {
  readonly " $data"?: AnalysisRecurringExpensesFragment$data;
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisRecurringExpensesFragment">;
};

import AnalysisRecurringExpensesPaginationQuery_graphql from './AnalysisRecurringExpensesPaginationQuery.graphql';

const node: ReaderFragment = (function(){
var v0 = [
  "recurringExpenses"
];
return {
  "argumentDefinitions": [
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
  "kind": "Fragment",
  "metadata": {
    "connection": [
      {
        "count": "first",
        "cursor": "after",
        "direction": "forward",
        "path": (v0/*:: as any*/)
      }
    ],
    "refetch": {
      "connection": {
        "forward": {
          "count": "first",
          "cursor": "after"
        },
        "backward": null,
        "path": (v0/*:: as any*/)
      },
      "fragmentPathInResult": [],
      "operation": AnalysisRecurringExpensesPaginationQuery_graphql
    }
  },
  "name": "AnalysisRecurringExpensesFragment",
  "selections": [
    {
      "alias": "recurringExpenses",
      "args": [
        {
          "kind": "Variable",
          "name": "householdId",
          "variableName": "householdId"
        },
        {
          "kind": "Literal",
          "name": "includeArchived",
          "value": false
        }
      ],
      "concreteType": "RecurringExpenseConnection",
      "kind": "LinkedField",
      "name": "__AnalysisRecurringExpensesFragment_recurringExpenses_connection",
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
    }
  ],
  "type": "Query",
  "abstractKey": null
};
})();

(node as any).hash = "6f53f1675c1140a7653c720381d5082f";

export default node;

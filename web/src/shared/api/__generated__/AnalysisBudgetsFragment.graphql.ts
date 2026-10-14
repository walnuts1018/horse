/**
 * @generated SignedSource<<b53663553139593c34ffaaa8a8ce58be>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type AnalysisBudgetsFragment$data = {
  readonly budgets: {
    readonly edges: ReadonlyArray<{
      readonly node: {
        readonly actualYen: any;
        readonly amountYen: any;
        readonly archived: boolean;
        readonly categoryId: string | null | undefined;
        readonly endsOn: string | null | undefined;
        readonly householdId: string;
        readonly id: string;
        readonly name: string;
        readonly remainingYen: any;
        readonly revision: number;
        readonly startsOn: string;
      };
    }>;
  };
  readonly " $fragmentType": "AnalysisBudgetsFragment";
};
export type AnalysisBudgetsFragment$key = {
  readonly " $data"?: AnalysisBudgetsFragment$data;
  readonly " $fragmentSpreads": FragmentRefs<"AnalysisBudgetsFragment">;
};

import AnalysisBudgetsPaginationQuery_graphql from './AnalysisBudgetsPaginationQuery.graphql';

const node: ReaderFragment = (function(){
var v0 = [
  "budgets"
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
    },
    {
      "defaultValue": null,
      "kind": "LocalArgument",
      "name": "period"
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
      "operation": AnalysisBudgetsPaginationQuery_graphql
    }
  },
  "name": "AnalysisBudgetsFragment",
  "selections": [
    {
      "alias": "budgets",
      "args": [
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
      ],
      "concreteType": "BudgetConnection",
      "kind": "LinkedField",
      "name": "__AnalysisBudgetsFragment_budgets_connection",
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
    }
  ],
  "type": "Query",
  "abstractKey": null
};
})();

(node as any).hash = "332aefca1f45590ff8b1a5761880f1f2";

export default node;

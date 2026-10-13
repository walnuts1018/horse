/**
 * @generated SignedSource<<38185a4030f0efe4c1dd078447fab756>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HomeSummaryQuery$variables = {
  householdId: string;
};
export type HomeSummaryQuery$data = {
  readonly homeSummary: {
    readonly dailySpending: ReadonlyArray<{
      readonly amountYen: any;
      readonly date: string;
    }>;
    readonly monthlyItemCount: any;
  };
};
export type HomeSummaryQuery = {
  response: HomeSummaryQuery$data;
  variables: HomeSummaryQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      }
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
        "name": "monthlyItemCount",
        "storageKey": null
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "HomeSummaryQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "HomeSummaryQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "a49fe875f99a3d4626f54f15ce06be8a",
    "id": null,
    "metadata": {},
    "name": "HomeSummaryQuery",
    "operationKind": "query",
    "text": "query HomeSummaryQuery(\n  $householdId: ID!\n) {\n  homeSummary(householdId: $householdId) {\n    dailySpending {\n      date\n      amountYen\n    }\n    monthlyItemCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "e6bd84da1f430beb3b4dda4ab4256a6a";

export default node;

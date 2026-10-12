/**
 * @generated SignedSource<<1163e43f8f193cee39c1a1f0702fed14>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type BudgetsQuery$variables = {
  householdId: string;
  period: string;
};
export type BudgetsQuery$data = {
  readonly budgets: ReadonlyArray<{
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
  }>;
};
export type BudgetsQuery = {
  response: BudgetsQuery$data;
  variables: BudgetsQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
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
    "alias": null,
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
    "concreteType": "Budget",
    "kind": "LinkedField",
    "name": "budgets",
    "plural": true,
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
    "name": "BudgetsQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "BudgetsQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "eea64b8ea00793b49b93840598aea09c",
    "id": null,
    "metadata": {},
    "name": "BudgetsQuery",
    "operationKind": "query",
    "text": "query BudgetsQuery(\n  $householdId: ID!\n  $period: String!\n) {\n  budgets(householdId: $householdId, period: $period) {\n    id\n    householdId\n    categoryId\n    name\n    amountYen\n    actualYen\n    remainingYen\n    startsOn\n    endsOn\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "6f2ddb9aec910b43587a306376404993";

export default node;

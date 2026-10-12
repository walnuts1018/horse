/**
 * @generated SignedSource<<23aabc502f9b287726a40b94567745d4>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SetBudgetInput = {
  amountYen: any;
  baseRevision?: number | null | undefined;
  budgetId?: string | null | undefined;
  categoryId?: string | null | undefined;
  endsOn?: string | null | undefined;
  householdId: string;
  name: string;
  startsOn: string;
};
export type SetBudgetMutation$variables = {
  input: SetBudgetInput;
};
export type SetBudgetMutation$data = {
  readonly setBudget: {
    readonly actualYen: any;
    readonly amountYen: any;
    readonly archived: boolean;
    readonly categoryId: string | null | undefined;
    readonly endsOn: string | null | undefined;
    readonly id: string;
    readonly name: string;
    readonly remainingYen: any;
    readonly revision: number;
    readonly startsOn: string;
  };
};
export type SetBudgetMutation = {
  response: SetBudgetMutation$data;
  variables: SetBudgetMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "input"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "input",
        "variableName": "input"
      }
    ],
    "concreteType": "Budget",
    "kind": "LinkedField",
    "name": "setBudget",
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
    "name": "SetBudgetMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetBudgetMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "12575e8d13214cd453b0aede81d90f5d",
    "id": null,
    "metadata": {},
    "name": "SetBudgetMutation",
    "operationKind": "mutation",
    "text": "mutation SetBudgetMutation(\n  $input: SetBudgetInput!\n) {\n  setBudget(input: $input) {\n    id\n    categoryId\n    name\n    amountYen\n    actualYen\n    remainingYen\n    startsOn\n    endsOn\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "de75385485bd1302f61563dbac655510";

export default node;

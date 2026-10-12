/**
 * @generated SignedSource<<da3ccee8343d8f2d003ce5a45f6430c0>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountBalancesQuery$variables = {
  householdId: string;
};
export type AccountBalancesQuery$data = {
  readonly accountBalances: ReadonlyArray<{
    readonly accountId: string;
    readonly balanceYen: any;
    readonly currency: string;
  }>;
};
export type AccountBalancesQuery = {
  response: AccountBalancesQuery$data;
  variables: AccountBalancesQuery$variables;
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
    "concreteType": "AccountBalance",
    "kind": "LinkedField",
    "name": "accountBalances",
    "plural": true,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "accountId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "balanceYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "currency",
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
    "name": "AccountBalancesQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AccountBalancesQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "95850fd4d12e905e72492d1d06579740",
    "id": null,
    "metadata": {},
    "name": "AccountBalancesQuery",
    "operationKind": "query",
    "text": "query AccountBalancesQuery(\n  $householdId: ID!\n) {\n  accountBalances(householdId: $householdId) {\n    accountId\n    balanceYen\n    currency\n  }\n}\n"
  }
};
})();

(node as any).hash = "067c39c49878005c878bc1ce324c23e7";

export default node;

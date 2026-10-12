/**
 * @generated SignedSource<<f087d131abee657bcdc919eefdbb6684>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountIncomesQuery$variables = {
  first?: number | null | undefined;
  householdId: string;
};
export type AccountIncomesQuery$data = {
  readonly accountIncomes: ReadonlyArray<{
    readonly accountId: string;
    readonly amountYen: any;
    readonly createdAt: string;
    readonly currency: string;
    readonly description: string;
    readonly householdId: string;
    readonly id: string;
    readonly occurredAt: string;
  }>;
};
export type AccountIncomesQuery = {
  response: AccountIncomesQuery$data;
  variables: AccountIncomesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v2 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "first",
        "variableName": "first"
      },
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      }
    ],
    "concreteType": "AccountIncome",
    "kind": "LinkedField",
    "name": "accountIncomes",
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
        "name": "accountId",
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
        "name": "occurredAt",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "description",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "createdAt",
        "storageKey": null
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "AccountIncomesQuery",
    "selections": (v2/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "AccountIncomesQuery",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "a2af544b586e5c6383b8185b82ed9180",
    "id": null,
    "metadata": {},
    "name": "AccountIncomesQuery",
    "operationKind": "query",
    "text": "query AccountIncomesQuery(\n  $householdId: ID!\n  $first: Int\n) {\n  accountIncomes(householdId: $householdId, first: $first) {\n    id\n    householdId\n    accountId\n    amountYen\n    currency\n    occurredAt\n    description\n    createdAt\n  }\n}\n"
  }
};
})();

(node as any).hash = "8855dced6b0806eee0bab6465ea0c6fa";

export default node;

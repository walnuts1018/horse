/**
 * @generated SignedSource<<6c35a7b285254244e67d52411fefc269>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountMovementKind = "CARD_BILL_PAYMENT" | "TRANSFER" | "%future added value";
export type AccountMovementsQuery$variables = {
  first?: number | null | undefined;
  householdId: string;
};
export type AccountMovementsQuery$data = {
  readonly accountMovements: ReadonlyArray<{
    readonly amountYen: any;
    readonly createdAt: string;
    readonly currency: string;
    readonly description: string;
    readonly fromAccountId: string;
    readonly householdId: string;
    readonly id: string;
    readonly kind: AccountMovementKind;
    readonly occurredAt: string;
    readonly toAccountId: string;
  }>;
};
export type AccountMovementsQuery = {
  response: AccountMovementsQuery$data;
  variables: AccountMovementsQuery$variables;
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
    "concreteType": "AccountMovement",
    "kind": "LinkedField",
    "name": "accountMovements",
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
        "name": "kind",
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
        "name": "fromAccountId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "toAccountId",
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
    "name": "AccountMovementsQuery",
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
    "name": "AccountMovementsQuery",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "8aab622f9497c2f97dcb047bb47520ed",
    "id": null,
    "metadata": {},
    "name": "AccountMovementsQuery",
    "operationKind": "query",
    "text": "query AccountMovementsQuery(\n  $householdId: ID!\n  $first: Int\n) {\n  accountMovements(householdId: $householdId, first: $first) {\n    id\n    householdId\n    kind\n    occurredAt\n    fromAccountId\n    toAccountId\n    amountYen\n    currency\n    description\n    createdAt\n  }\n}\n"
  }
};
})();

(node as any).hash = "65888b8b2b7ba28d29a0004715279818";

export default node;

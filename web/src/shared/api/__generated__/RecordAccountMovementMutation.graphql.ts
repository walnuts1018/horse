/**
 * @generated SignedSource<<f2cc89cb3fcd385313606757503c7ae7>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountMovementKind = "CARD_BILL_PAYMENT" | "TRANSFER" | "%future added value";
export type RecordAccountMovementInput = {
  amountYen: any;
  description: string;
  fromAccountId: string;
  householdId: string;
  idempotencyKey: string;
  kind: AccountMovementKind;
  occurredAt: string;
  toAccountId: string;
};
export type RecordAccountMovementMutation$variables = {
  input: RecordAccountMovementInput;
};
export type RecordAccountMovementMutation$data = {
  readonly recordAccountMovement: {
    readonly amountYen: any;
    readonly description: string;
    readonly fromAccountId: string;
    readonly id: string;
    readonly kind: AccountMovementKind;
    readonly occurredAt: string;
    readonly toAccountId: string;
  };
};
export type RecordAccountMovementMutation = {
  response: RecordAccountMovementMutation$data;
  variables: RecordAccountMovementMutation$variables;
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
    "concreteType": "AccountMovement",
    "kind": "LinkedField",
    "name": "recordAccountMovement",
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
        "name": "description",
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
    "name": "RecordAccountMovementMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecordAccountMovementMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "941cfd7da3f1cbac61a03deece7c0d8a",
    "id": null,
    "metadata": {},
    "name": "RecordAccountMovementMutation",
    "operationKind": "mutation",
    "text": "mutation RecordAccountMovementMutation(\n  $input: RecordAccountMovementInput!\n) {\n  recordAccountMovement(input: $input) {\n    id\n    kind\n    occurredAt\n    fromAccountId\n    toAccountId\n    amountYen\n    description\n  }\n}\n"
  }
};
})();

(node as any).hash = "93801d1d8c311b4e4c955e1858016b93";

export default node;

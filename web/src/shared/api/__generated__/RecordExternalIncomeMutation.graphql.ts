/**
 * @generated SignedSource<<9b0f305016b23019bccd11087e015c31>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type RecordExternalIncomeInput = {
  accountId: string;
  amountYen: any;
  description: string;
  householdId: string;
  idempotencyKey: string;
  occurredAt: string;
};
export type RecordExternalIncomeMutation$variables = {
  input: RecordExternalIncomeInput;
};
export type RecordExternalIncomeMutation$data = {
  readonly recordExternalIncome: {
    readonly accountId: string;
    readonly amountYen: any;
    readonly currency: string;
    readonly description: string;
    readonly householdId: string;
    readonly id: string;
    readonly occurredAt: string;
  };
};
export type RecordExternalIncomeMutation = {
  response: RecordExternalIncomeMutation$data;
  variables: RecordExternalIncomeMutation$variables;
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
    "concreteType": "AccountIncome",
    "kind": "LinkedField",
    "name": "recordExternalIncome",
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
        "name": "accountId",
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
    "name": "RecordExternalIncomeMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecordExternalIncomeMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "46fb2404fe8f7716679b4db5fbb2c7f4",
    "id": null,
    "metadata": {},
    "name": "RecordExternalIncomeMutation",
    "operationKind": "mutation",
    "text": "mutation RecordExternalIncomeMutation(\n  $input: RecordExternalIncomeInput!\n) {\n  recordExternalIncome(input: $input) {\n    id\n    householdId\n    accountId\n    currency\n    occurredAt\n    amountYen\n    description\n  }\n}\n"
  }
};
})();

(node as any).hash = "f85fdb15564e0e4d188f25fab8154ff8";

export default node;

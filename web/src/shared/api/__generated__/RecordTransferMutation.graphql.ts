/**
 * @generated SignedSource<<b791227de06e27994d2bc0589be23730>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type TransferKind = "ADVANCE" | "GIFT" | "LOAN" | "REPAYMENT" | "%future added value";
export type RecordTransferInput = {
  amountYen: any;
  description: string;
  destinationAccountId?: string | null | undefined;
  fromMemberId: string;
  householdId: string;
  idempotencyKey: string;
  kind: TransferKind;
  loanId?: string | null | undefined;
  occurredAt: string;
  paymentAccountId?: string | null | undefined;
  toMemberId: string;
};
export type RecordTransferMutation$variables = {
  input: RecordTransferInput;
};
export type RecordTransferMutation$data = {
  readonly recordTransfer: {
    readonly id: string;
    readonly loanId: string | null | undefined;
  };
};
export type RecordTransferMutation = {
  response: RecordTransferMutation$data;
  variables: RecordTransferMutation$variables;
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
    "concreteType": "LedgerEntry",
    "kind": "LinkedField",
    "name": "recordTransfer",
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
        "name": "loanId",
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
    "name": "RecordTransferMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecordTransferMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "a1a3526fc6216b1dcfa6dddd2eecbfb6",
    "id": null,
    "metadata": {},
    "name": "RecordTransferMutation",
    "operationKind": "mutation",
    "text": "mutation RecordTransferMutation(\n  $input: RecordTransferInput!\n) {\n  recordTransfer(input: $input) {\n    id\n    loanId\n  }\n}\n"
  }
};
})();

(node as any).hash = "3a4ea75a1b0155cdda78a8ae370710c8";

export default node;

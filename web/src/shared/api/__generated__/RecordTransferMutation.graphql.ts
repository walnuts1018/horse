/**
 * @generated SignedSource<<ad98aec7899e56580d86a639981e54d0>>
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
  fromMemberId: string;
  householdId: string;
  idempotencyKey: string;
  kind: TransferKind;
  loanId?: string | null | undefined;
  occurredAt: string;
  toMemberId: string;
};
export type RecordTransferMutation$variables = {
  input: RecordTransferInput;
};
export type RecordTransferMutation$data = {
  readonly recordTransfer: {
    readonly id: string;
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
    "cacheID": "c0e9a36008a84f29241c0269b5e396d6",
    "id": null,
    "metadata": {},
    "name": "RecordTransferMutation",
    "operationKind": "mutation",
    "text": "mutation RecordTransferMutation(\n  $input: RecordTransferInput!\n) {\n  recordTransfer(input: $input) {\n    id\n  }\n}\n"
  }
};
})();

(node as any).hash = "d32429ff5ddae1d52a34fcf81709cb1e";

export default node;

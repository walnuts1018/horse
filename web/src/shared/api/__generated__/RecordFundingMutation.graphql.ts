/**
 * @generated SignedSource<<b59db28b50db1dcb43109184a8b4f23e>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type RecordFundingInput = {
  amountYen: any;
  destinationAccountId?: string | null | undefined;
  householdId: string;
  idempotencyKey: string;
  memberId: string;
  occurredAt: string;
  poolId: string;
  sourceAccountId?: string | null | undefined;
};
export type RecordFundingMutation$variables = {
  input: RecordFundingInput;
};
export type RecordFundingMutation$data = {
  readonly recordFunding: {
    readonly id: string;
  };
};
export type RecordFundingMutation = {
  response: RecordFundingMutation$data;
  variables: RecordFundingMutation$variables;
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
    "name": "recordFunding",
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
    "name": "RecordFundingMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecordFundingMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "219ea0d517261f0b877d9380875b56ad",
    "id": null,
    "metadata": {},
    "name": "RecordFundingMutation",
    "operationKind": "mutation",
    "text": "mutation RecordFundingMutation(\n  $input: RecordFundingInput!\n) {\n  recordFunding(input: $input) {\n    id\n  }\n}\n"
  }
};
})();

(node as any).hash = "d7ab0fe4961973b159859dee38115e93";

export default node;

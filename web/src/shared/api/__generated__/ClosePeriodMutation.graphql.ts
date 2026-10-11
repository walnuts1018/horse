/**
 * @generated SignedSource<<b2e54413e925acd47698804a6f3dca9e>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type ClosePeriodInput = {
  expectedRevision: number;
  householdId: string;
  period: string;
};
export type ClosePeriodMutation$variables = {
  input: ClosePeriodInput;
};
export type ClosePeriodMutation$data = {
  readonly closePeriod: {
    readonly period: string;
    readonly revision: number;
    readonly status: string;
  };
};
export type ClosePeriodMutation = {
  response: ClosePeriodMutation$data;
  variables: ClosePeriodMutation$variables;
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
    "concreteType": "Settlement",
    "kind": "LinkedField",
    "name": "closePeriod",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "period",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "status",
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
    "name": "ClosePeriodMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "ClosePeriodMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "374a9f0e58d7a8d55432c3e5d9bb06c9",
    "id": null,
    "metadata": {},
    "name": "ClosePeriodMutation",
    "operationKind": "mutation",
    "text": "mutation ClosePeriodMutation(\n  $input: ClosePeriodInput!\n) {\n  closePeriod(input: $input) {\n    period\n    status\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "5616e5b6862a0ee15f3d58e0ad13bb87";

export default node;

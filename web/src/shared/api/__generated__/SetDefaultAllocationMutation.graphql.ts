/**
 * @generated SignedSource<<4e3e6c692af6dc0d68aae342d9b618fa>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SetDefaultAllocationInput = {
  allocations: ReadonlyArray<AllocationInput>;
  householdId: string;
};
export type AllocationInput = {
  poolId: string;
  weight: any;
};
export type SetDefaultAllocationMutation$variables = {
  input: SetDefaultAllocationInput;
};
export type SetDefaultAllocationMutation$data = {
  readonly setDefaultAllocation: {
    readonly id: string;
    readonly revision: number;
  };
};
export type SetDefaultAllocationMutation = {
  response: SetDefaultAllocationMutation$data;
  variables: SetDefaultAllocationMutation$variables;
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
    "concreteType": "Household",
    "kind": "LinkedField",
    "name": "setDefaultAllocation",
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
    "name": "SetDefaultAllocationMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetDefaultAllocationMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "f24270cce1303283f1088b81049e3f3c",
    "id": null,
    "metadata": {},
    "name": "SetDefaultAllocationMutation",
    "operationKind": "mutation",
    "text": "mutation SetDefaultAllocationMutation(\n  $input: SetDefaultAllocationInput!\n) {\n  setDefaultAllocation(input: $input) {\n    id\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "8a20504e48c8981fc2d7eccac6079bed";

export default node;

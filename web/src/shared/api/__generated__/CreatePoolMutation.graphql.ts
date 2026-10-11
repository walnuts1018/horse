/**
 * @generated SignedSource<<73630abba956a70775bcfd0ae732c2aa>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PoolKind = "PERSONAL" | "SHARED" | "%future added value";
export type CreatePoolInput = {
  householdId: string;
  kind: PoolKind;
  name: string;
};
export type CreatePoolMutation$variables = {
  input: CreatePoolInput;
};
export type CreatePoolMutation$data = {
  readonly createPool: {
    readonly id: string;
  };
};
export type CreatePoolMutation = {
  response: CreatePoolMutation$data;
  variables: CreatePoolMutation$variables;
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
    "concreteType": "Pool",
    "kind": "LinkedField",
    "name": "createPool",
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
    "name": "CreatePoolMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreatePoolMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "b628a46fbdb5f09503ee3020f0ec9dfa",
    "id": null,
    "metadata": {},
    "name": "CreatePoolMutation",
    "operationKind": "mutation",
    "text": "mutation CreatePoolMutation(\n  $input: CreatePoolInput!\n) {\n  createPool(input: $input) {\n    id\n  }\n}\n"
  }
};
})();

(node as any).hash = "f7e301e0720a5e51517d865ae1b94d9d";

export default node;

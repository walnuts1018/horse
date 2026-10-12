/**
 * @generated SignedSource<<c68ced987ec9d502b861b12b49fdd8cd>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type CreateTagInput = {
  householdId: string;
  name: string;
};
export type CreateTagMutation$variables = {
  input: CreateTagInput;
};
export type CreateTagMutation$data = {
  readonly createTag: {
    readonly archived: boolean;
    readonly householdId: string;
    readonly id: string;
    readonly name: string;
    readonly revision: number;
  };
};
export type CreateTagMutation = {
  response: CreateTagMutation$data;
  variables: CreateTagMutation$variables;
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
    "concreteType": "Tag",
    "kind": "LinkedField",
    "name": "createTag",
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
        "name": "name",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "archived",
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
    "name": "CreateTagMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateTagMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "73456e3d84bec10ff783891efa59b59f",
    "id": null,
    "metadata": {},
    "name": "CreateTagMutation",
    "operationKind": "mutation",
    "text": "mutation CreateTagMutation(\n  $input: CreateTagInput!\n) {\n  createTag(input: $input) {\n    id\n    householdId\n    name\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "f3297a3ad9f5360d85d043714fce9305";

export default node;

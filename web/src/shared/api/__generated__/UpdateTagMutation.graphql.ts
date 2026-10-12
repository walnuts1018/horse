/**
 * @generated SignedSource<<7c25900b963e92581e39e581d4fd9e35>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type UpdateTagInput = {
  baseRevision: number;
  name: string;
  tagId: string;
};
export type UpdateTagMutation$variables = {
  input: UpdateTagInput;
};
export type UpdateTagMutation$data = {
  readonly updateTag: {
    readonly archived: boolean;
    readonly householdId: string;
    readonly id: string;
    readonly name: string;
    readonly revision: number;
  };
};
export type UpdateTagMutation = {
  response: UpdateTagMutation$data;
  variables: UpdateTagMutation$variables;
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
    "name": "updateTag",
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
    "name": "UpdateTagMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "UpdateTagMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "d634c6f73b3e882d9e928382b9be8941",
    "id": null,
    "metadata": {},
    "name": "UpdateTagMutation",
    "operationKind": "mutation",
    "text": "mutation UpdateTagMutation(\n  $input: UpdateTagInput!\n) {\n  updateTag(input: $input) {\n    id\n    householdId\n    name\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "f1ad082dab2626f6bc034467850c34d2";

export default node;

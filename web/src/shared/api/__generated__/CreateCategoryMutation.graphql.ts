/**
 * @generated SignedSource<<952f44855fbab2bbc41bffbf9436a120>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type CreateCategoryInput = {
  householdId: string;
  name: string;
  parentId?: string | null | undefined;
};
export type CreateCategoryMutation$variables = {
  input: CreateCategoryInput;
};
export type CreateCategoryMutation$data = {
  readonly createCategory: {
    readonly id: string;
  };
};
export type CreateCategoryMutation = {
  response: CreateCategoryMutation$data;
  variables: CreateCategoryMutation$variables;
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
    "concreteType": "Category",
    "kind": "LinkedField",
    "name": "createCategory",
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
    "name": "CreateCategoryMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateCategoryMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "7c456adc36e3dbc181d256f018b21a46",
    "id": null,
    "metadata": {},
    "name": "CreateCategoryMutation",
    "operationKind": "mutation",
    "text": "mutation CreateCategoryMutation(\n  $input: CreateCategoryInput!\n) {\n  createCategory(input: $input) {\n    id\n  }\n}\n"
  }
};
})();

(node as any).hash = "ffe53f97fb3153ba5b08afc7753e912a";

export default node;

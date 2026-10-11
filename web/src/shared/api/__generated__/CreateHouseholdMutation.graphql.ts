/**
 * @generated SignedSource<<19082df2ac19a51f8f8e10ed44b5ac0f>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type CreateHouseholdInput = {
  memberNames: ReadonlyArray<string>;
  name: string;
};
export type CreateHouseholdMutation$variables = {
  input: CreateHouseholdInput;
};
export type CreateHouseholdMutation$data = {
  readonly createHousehold: {
    readonly id: string;
  };
};
export type CreateHouseholdMutation = {
  response: CreateHouseholdMutation$data;
  variables: CreateHouseholdMutation$variables;
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
    "name": "createHousehold",
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
    "name": "CreateHouseholdMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateHouseholdMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "b9d9023cefa843969fd009ac7222dd78",
    "id": null,
    "metadata": {},
    "name": "CreateHouseholdMutation",
    "operationKind": "mutation",
    "text": "mutation CreateHouseholdMutation(\n  $input: CreateHouseholdInput!\n) {\n  createHousehold(input: $input) {\n    id\n  }\n}\n"
  }
};
})();

(node as any).hash = "3f483f030d77297cd6bdf2dc0644fb1a";

export default node;

/**
 * @generated SignedSource<<cb7eae377b404a20c977ed56ac6abecb>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type CreateProductConceptInput = {
  categoryId?: string | null | undefined;
  householdId: string;
  name: string;
};
export type CreateProductConceptMutation$variables = {
  input: CreateProductConceptInput;
};
export type CreateProductConceptMutation$data = {
  readonly createProductConcept: {
    readonly id: string;
    readonly name: string;
  };
};
export type CreateProductConceptMutation = {
  response: CreateProductConceptMutation$data;
  variables: CreateProductConceptMutation$variables;
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
    "concreteType": "ProductConcept",
    "kind": "LinkedField",
    "name": "createProductConcept",
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
        "name": "name",
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
    "name": "CreateProductConceptMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateProductConceptMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "ba1024381281c71e609d4e554a0d1588",
    "id": null,
    "metadata": {},
    "name": "CreateProductConceptMutation",
    "operationKind": "mutation",
    "text": "mutation CreateProductConceptMutation(\n  $input: CreateProductConceptInput!\n) {\n  createProductConcept(input: $input) {\n    id\n    name\n  }\n}\n"
  }
};
})();

(node as any).hash = "36c813e3932a4a55055ce5f31a514d3b";

export default node;

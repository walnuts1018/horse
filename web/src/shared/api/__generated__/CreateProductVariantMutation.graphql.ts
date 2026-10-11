/**
 * @generated SignedSource<<af0ac4ec1bfeeec68991092af0c7eab5>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type CreateProductVariantInput = {
  barcode?: string | null | undefined;
  brand?: string | null | undefined;
  conceptId: string;
  householdId: string;
  name: string;
  packageSize?: string | null | undefined;
};
export type CreateProductVariantMutation$variables = {
  input: CreateProductVariantInput;
};
export type CreateProductVariantMutation$data = {
  readonly createProductVariant: {
    readonly id: string;
    readonly name: string;
  };
};
export type CreateProductVariantMutation = {
  response: CreateProductVariantMutation$data;
  variables: CreateProductVariantMutation$variables;
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
    "concreteType": "ProductVariant",
    "kind": "LinkedField",
    "name": "createProductVariant",
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
    "name": "CreateProductVariantMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateProductVariantMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "3237421a6ecc323f22274a85d4281ec1",
    "id": null,
    "metadata": {},
    "name": "CreateProductVariantMutation",
    "operationKind": "mutation",
    "text": "mutation CreateProductVariantMutation(\n  $input: CreateProductVariantInput!\n) {\n  createProductVariant(input: $input) {\n    id\n    name\n  }\n}\n"
  }
};
})();

(node as any).hash = "aa9005b11fb3fe581df30d3e72ae3a81";

export default node;

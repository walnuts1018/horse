/**
 * @generated SignedSource<<306d2ae6abd872507e1b04c47329a350>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
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
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
      readonly name: string;
    };
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
    "concreteType": null,
    "kind": "LinkedField",
    "name": "createProductVariant",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "__typename",
        "storageKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "ProductVariant",
            "kind": "LinkedField",
            "name": "result",
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
        ],
        "type": "CreateProductVariantSuccess",
        "abstractKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "code",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "message",
            "storageKey": null
          }
        ],
        "type": "MutationError",
        "abstractKey": "__isMutationError"
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
    "cacheID": "8179717e2085e262ff4b26c32aa11855",
    "id": null,
    "metadata": {},
    "name": "CreateProductVariantMutation",
    "operationKind": "mutation",
    "text": "mutation CreateProductVariantMutation(\n  $input: CreateProductVariantInput!\n) {\n  createProductVariant(input: $input) {\n    __typename\n    ... on CreateProductVariantSuccess {\n      result {\n        id\n        name\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "01f519782bc0f7cb87e498a55324fc8d";

export default node;

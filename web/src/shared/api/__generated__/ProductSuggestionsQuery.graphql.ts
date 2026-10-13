/**
 * @generated SignedSource<<29706b019f0d9f5312ec1e9e3206c9f3>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type ProductSuggestionsQuery$variables = {
  first: number;
  householdId: string;
  merchant?: string | null | undefined;
  query: string;
};
export type ProductSuggestionsQuery$data = {
  readonly productSuggestions: ReadonlyArray<{
    readonly concept: {
      readonly categoryId: string | null | undefined;
      readonly id: string;
      readonly name: string;
    };
    readonly matchedName: string;
    readonly score: number;
    readonly variant: {
      readonly brand: string | null | undefined;
      readonly id: string;
      readonly name: string;
      readonly packageSize: string | null | undefined;
    };
  }>;
};
export type ProductSuggestionsQuery = {
  response: ProductSuggestionsQuery$data;
  variables: ProductSuggestionsQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "merchant"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "query"
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v6 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "first",
        "variableName": "first"
      },
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      },
      {
        "kind": "Variable",
        "name": "merchant",
        "variableName": "merchant"
      },
      {
        "kind": "Variable",
        "name": "query",
        "variableName": "query"
      }
    ],
    "concreteType": "ProductSuggestion",
    "kind": "LinkedField",
    "name": "productSuggestions",
    "plural": true,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "ProductConcept",
        "kind": "LinkedField",
        "name": "concept",
        "plural": false,
        "selections": [
          (v4/*:: as any*/),
          (v5/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "categoryId",
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "ProductVariant",
        "kind": "LinkedField",
        "name": "variant",
        "plural": false,
        "selections": [
          (v4/*:: as any*/),
          (v5/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "brand",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "packageSize",
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "matchedName",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "score",
        "storageKey": null
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v3/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "ProductSuggestionsQuery",
    "selections": (v6/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v3/*:: as any*/),
      (v2/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "ProductSuggestionsQuery",
    "selections": (v6/*:: as any*/)
  },
  "params": {
    "cacheID": "e3f5247eb0335e7e550f2d90a3ef74da",
    "id": null,
    "metadata": {},
    "name": "ProductSuggestionsQuery",
    "operationKind": "query",
    "text": "query ProductSuggestionsQuery(\n  $householdId: ID!\n  $query: String!\n  $merchant: String\n  $first: Int!\n) {\n  productSuggestions(householdId: $householdId, query: $query, merchant: $merchant, first: $first) {\n    concept {\n      id\n      name\n      categoryId\n    }\n    variant {\n      id\n      name\n      brand\n      packageSize\n    }\n    matchedName\n    score\n  }\n}\n"
  }
};
})();

(node as any).hash = "6bcd9a69ce858dfec3c59111a77563f6";

export default node;

/**
 * @generated SignedSource<<49f64e8bce6e1b6b515f9e789785421c>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type ProductCatalogQuery$variables = {
  householdId: string;
};
export type ProductCatalogQuery$data = {
  readonly productCatalog: ReadonlyArray<{
    readonly archived: boolean;
    readonly categoryId: string | null | undefined;
    readonly householdId: string;
    readonly id: string;
    readonly name: string;
    readonly normalizedName: string;
    readonly revision: number;
    readonly variants: ReadonlyArray<{
      readonly aliases: ReadonlyArray<{
        readonly alias: string;
        readonly id: string;
        readonly merchantKey: string;
        readonly revision: number;
      }>;
      readonly archived: boolean;
      readonly barcode: string | null | undefined;
      readonly brand: string;
      readonly conceptId: string;
      readonly householdId: string;
      readonly id: string;
      readonly name: string;
      readonly normalizedName: string;
      readonly packageSize: string;
      readonly revision: number;
    }>;
  }>;
};
export type ProductCatalogQuery = {
  response: ProductCatalogQuery$data;
  variables: ProductCatalogQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  }
],
v1 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "normalizedName",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "archived",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v7 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      }
    ],
    "concreteType": "ProductConcept",
    "kind": "LinkedField",
    "name": "productCatalog",
    "plural": true,
    "selections": [
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v3/*:: as any*/),
      (v4/*:: as any*/),
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "categoryId",
        "storageKey": null
      },
      (v5/*:: as any*/),
      (v6/*:: as any*/),
      {
        "alias": null,
        "args": null,
        "concreteType": "ProductVariant",
        "kind": "LinkedField",
        "name": "variants",
        "plural": true,
        "selections": [
          (v1/*:: as any*/),
          (v2/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "conceptId",
            "storageKey": null
          },
          (v3/*:: as any*/),
          (v4/*:: as any*/),
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
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "barcode",
            "storageKey": null
          },
          (v5/*:: as any*/),
          (v6/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "concreteType": "ProductAlias",
            "kind": "LinkedField",
            "name": "aliases",
            "plural": true,
            "selections": [
              (v1/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "alias",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "merchantKey",
                "storageKey": null
              },
              (v6/*:: as any*/)
            ],
            "storageKey": null
          }
        ],
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
    "name": "ProductCatalogQuery",
    "selections": (v7/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "ProductCatalogQuery",
    "selections": (v7/*:: as any*/)
  },
  "params": {
    "cacheID": "e7228467a4ec2daa9c49435720f6daa4",
    "id": null,
    "metadata": {},
    "name": "ProductCatalogQuery",
    "operationKind": "query",
    "text": "query ProductCatalogQuery(\n  $householdId: ID!\n) {\n  productCatalog(householdId: $householdId) {\n    id\n    householdId\n    name\n    normalizedName\n    categoryId\n    archived\n    revision\n    variants {\n      id\n      householdId\n      conceptId\n      name\n      normalizedName\n      brand\n      packageSize\n      barcode\n      archived\n      revision\n      aliases {\n        id\n        alias\n        merchantKey\n        revision\n      }\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "791d22164d95446d45cc702422794db7";

export default node;

/**
 * @generated SignedSource<<3e69eaab3689d5d5be69620119649adb>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type ProductCatalogQuery$variables = {
  after?: string | null | undefined;
  first: number;
  householdId: string;
};
export type ProductCatalogQuery$data = {
  readonly productCatalog: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
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
          readonly brand: string | null | undefined;
          readonly conceptId: string;
          readonly householdId: string;
          readonly id: string;
          readonly name: string;
          readonly normalizedName: string;
          readonly packageSize: string | null | undefined;
          readonly revision: number;
        }>;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
  };
};
export type ProductCatalogQuery = {
  response: ProductCatalogQuery$data;
  variables: ProductCatalogQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "after"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "normalizedName",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "archived",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v9 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "after",
        "variableName": "after"
      },
      {
        "kind": "Variable",
        "name": "first",
        "variableName": "first"
      },
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      }
    ],
    "concreteType": "ProductConceptConnection",
    "kind": "LinkedField",
    "name": "productCatalog",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "ProductConceptEdge",
        "kind": "LinkedField",
        "name": "edges",
        "plural": true,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "cursor",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "ProductConcept",
            "kind": "LinkedField",
            "name": "node",
            "plural": false,
            "selections": [
              (v3/*:: as any*/),
              (v4/*:: as any*/),
              (v5/*:: as any*/),
              (v6/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "categoryId",
                "storageKey": null
              },
              (v7/*:: as any*/),
              (v8/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": "ProductVariant",
                "kind": "LinkedField",
                "name": "variants",
                "plural": true,
                "selections": [
                  (v3/*:: as any*/),
                  (v4/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "conceptId",
                    "storageKey": null
                  },
                  (v5/*:: as any*/),
                  (v6/*:: as any*/),
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
                  (v7/*:: as any*/),
                  (v8/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "ProductAlias",
                    "kind": "LinkedField",
                    "name": "aliases",
                    "plural": true,
                    "selections": [
                      (v3/*:: as any*/),
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
                      (v8/*:: as any*/)
                    ],
                    "storageKey": null
                  }
                ],
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "PageInfo",
        "kind": "LinkedField",
        "name": "pageInfo",
        "plural": false,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "endCursor",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "hasNextPage",
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
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "ProductCatalogQuery",
    "selections": (v9/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "ProductCatalogQuery",
    "selections": (v9/*:: as any*/)
  },
  "params": {
    "cacheID": "7a17d6dd66bc5e82c3699b98777aef80",
    "id": null,
    "metadata": {},
    "name": "ProductCatalogQuery",
    "operationKind": "query",
    "text": "query ProductCatalogQuery(\n  $householdId: ID!\n  $first: Int!\n  $after: String\n) {\n  productCatalog(householdId: $householdId, first: $first, after: $after) {\n    edges {\n      cursor\n      node {\n        id\n        householdId\n        name\n        normalizedName\n        categoryId\n        archived\n        revision\n        variants {\n          id\n          householdId\n          conceptId\n          name\n          normalizedName\n          brand\n          packageSize\n          barcode\n          archived\n          revision\n          aliases {\n            id\n            alias\n            merchantKey\n            revision\n          }\n        }\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "8df7337c3505db60cc06d26585706db8";

export default node;

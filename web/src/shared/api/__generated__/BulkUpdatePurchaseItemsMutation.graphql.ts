/**
 * @generated SignedSource<<201061a01b0077135dcb0e09525f5e51>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type BulkUpdatePurchaseItemsInput = {
  changedFields: ReadonlyArray<string>;
  patch: BulkPurchaseItemPatch;
  targets: ReadonlyArray<PurchaseItemTargetInput>;
};
export type PurchaseItemTargetInput = {
  baseRevision: number;
  itemId: string;
};
export type BulkPurchaseItemPatch = {
  allocations?: ReadonlyArray<AllocationInput> | null | undefined;
  categoryId?: string | null | undefined;
  productVariantId?: string | null | undefined;
  tagIds?: ReadonlyArray<string> | null | undefined;
};
export type AllocationInput = {
  poolId: string;
  weight: any;
};
export type BulkUpdatePurchaseItemsMutation$variables = {
  input: BulkUpdatePurchaseItemsInput;
};
export type BulkUpdatePurchaseItemsMutation$data = {
  readonly bulkUpdatePurchaseItems: ReadonlyArray<{
    readonly allocations: ReadonlyArray<{
      readonly poolId: string;
      readonly weight: any;
    }>;
    readonly categoryId: string | null | undefined;
    readonly id: string;
    readonly productVariantId: string | null | undefined;
    readonly revision: number;
    readonly tags: ReadonlyArray<{
      readonly archived: boolean;
      readonly id: string;
      readonly name: string;
      readonly revision: number;
    }>;
  }>;
};
export type BulkUpdatePurchaseItemsMutation = {
  response: BulkUpdatePurchaseItemsMutation$data;
  variables: BulkUpdatePurchaseItemsMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "input"
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
  "name": "revision",
  "storageKey": null
},
v3 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "input",
        "variableName": "input"
      }
    ],
    "concreteType": "PurchaseItem",
    "kind": "LinkedField",
    "name": "bulkUpdatePurchaseItems",
    "plural": true,
    "selections": [
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "categoryId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "productVariantId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "Allocation",
        "kind": "LinkedField",
        "name": "allocations",
        "plural": true,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "poolId",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "weight",
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "Tag",
        "kind": "LinkedField",
        "name": "tags",
        "plural": true,
        "selections": [
          (v1/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "name",
            "storageKey": null
          },
          (v2/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "archived",
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
    "name": "BulkUpdatePurchaseItemsMutation",
    "selections": (v3/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "BulkUpdatePurchaseItemsMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "93f62a8300b894faee5c978548d4e5e1",
    "id": null,
    "metadata": {},
    "name": "BulkUpdatePurchaseItemsMutation",
    "operationKind": "mutation",
    "text": "mutation BulkUpdatePurchaseItemsMutation(\n  $input: BulkUpdatePurchaseItemsInput!\n) {\n  bulkUpdatePurchaseItems(input: $input) {\n    id\n    revision\n    categoryId\n    productVariantId\n    allocations {\n      poolId\n      weight\n    }\n    tags {\n      id\n      name\n      revision\n      archived\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "cbe4cc955937112f6fc8e256a66153ff";

export default node;

/**
 * @generated SignedSource<<0d1fa2a4058c17d31553345455e9d093>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
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
  readonly bulkUpdatePurchaseItems: {
    readonly __typename: "BulkUpdatePurchaseItemsSuccess";
    readonly items: ReadonlyArray<{
      readonly allocations: ReadonlyArray<{
        readonly amountYen: any | null | undefined;
        readonly poolId: string;
        readonly poolName: string | null | undefined;
        readonly weight: any;
      }>;
      readonly amountYen: any;
      readonly categoryId: string | null | undefined;
      readonly currency: string;
      readonly effectiveUnitPrice: {
        readonly denominator: string;
        readonly numeratorYen: string;
      } | null | undefined;
      readonly id: string;
      readonly name: string;
      readonly printedUnitPrice: {
        readonly amountYen: any;
        readonly basis: {
          readonly denominator: string;
          readonly numerator: string;
          readonly unit: string;
        };
      } | null | undefined;
      readonly productVariantId: string | null | undefined;
      readonly quantity: {
        readonly denominator: string;
        readonly numerator: string;
        readonly unit: string;
      } | null | undefined;
      readonly revision: number;
      readonly tags: ReadonlyArray<{
        readonly archived: boolean;
        readonly householdId: string;
        readonly id: string;
        readonly name: string;
        readonly revision: number;
      }>;
    }>;
  } | {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
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
  "name": "name",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "denominator",
  "storageKey": null
},
v5 = [
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "numerator",
    "storageKey": null
  },
  (v4/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "unit",
    "storageKey": null
  }
],
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
        "name": "input",
        "variableName": "input"
      }
    ],
    "concreteType": null,
    "kind": "LinkedField",
    "name": "bulkUpdatePurchaseItems",
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
            "concreteType": "PurchaseItem",
            "kind": "LinkedField",
            "name": "items",
            "plural": true,
            "selections": [
              (v1/*:: as any*/),
              (v2/*:: as any*/),
              (v3/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "currency",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "concreteType": "PurchaseQuantity",
                "kind": "LinkedField",
                "name": "quantity",
                "plural": false,
                "selections": (v5/*:: as any*/),
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "concreteType": "PrintedUnitPrice",
                "kind": "LinkedField",
                "name": "printedUnitPrice",
                "plural": false,
                "selections": [
                  (v3/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "PurchaseQuantity",
                    "kind": "LinkedField",
                    "name": "basis",
                    "plural": false,
                    "selections": (v5/*:: as any*/),
                    "storageKey": null
                  }
                ],
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "concreteType": "EffectiveUnitPrice",
                "kind": "LinkedField",
                "name": "effectiveUnitPrice",
                "plural": false,
                "selections": [
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "numeratorYen",
                    "storageKey": null
                  },
                  (v4/*:: as any*/)
                ],
                "storageKey": null
              },
              (v6/*:: as any*/),
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
                    "name": "poolName",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "weight",
                    "storageKey": null
                  },
                  (v3/*:: as any*/)
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
                    "name": "householdId",
                    "storageKey": null
                  },
                  (v2/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "archived",
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
        "type": "BulkUpdatePurchaseItemsSuccess",
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
        "abstractKey": null
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
    "selections": (v7/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "BulkUpdatePurchaseItemsMutation",
    "selections": (v7/*:: as any*/)
  },
  "params": {
    "cacheID": "a830711051cfb01abbd30ce1dd501e98",
    "id": null,
    "metadata": {},
    "name": "BulkUpdatePurchaseItemsMutation",
    "operationKind": "mutation",
    "text": "mutation BulkUpdatePurchaseItemsMutation(\n  $input: BulkUpdatePurchaseItemsInput!\n) {\n  bulkUpdatePurchaseItems(input: $input) {\n    __typename\n    ... on BulkUpdatePurchaseItemsSuccess {\n      items {\n        id\n        name\n        amountYen\n        currency\n        quantity {\n          numerator\n          denominator\n          unit\n        }\n        printedUnitPrice {\n          amountYen\n          basis {\n            numerator\n            denominator\n            unit\n          }\n        }\n        effectiveUnitPrice {\n          numeratorYen\n          denominator\n        }\n        revision\n        categoryId\n        productVariantId\n        allocations {\n          poolId\n          poolName\n          weight\n          amountYen\n        }\n        tags {\n          id\n          householdId\n          name\n          archived\n          revision\n        }\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "7863aebc9d7c573032b6160eb05a9da1";

export default node;

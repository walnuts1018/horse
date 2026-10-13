/**
 * @generated SignedSource<<338b22abe61810a87d4abcf58982d296>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type UpdatePurchaseItemInput = {
  allocations?: ReadonlyArray<UpdatePurchaseItemAllocationInput> | null | undefined;
  amountYen?: any | null | undefined;
  baseRevision: number;
  categoryId?: string | null | undefined;
  itemId: string;
  name?: string | null | undefined;
  printedUnitPrice?: UpdatePurchaseItemPrintedUnitPriceInput | null | undefined;
  productLearning?: ProductLearningConfigurationInput | null | undefined;
  productVariantId?: string | null | undefined;
  quantity?: UpdatePurchaseItemQuantityInput | null | undefined;
};
export type UpdatePurchaseItemQuantityInput = {
  denominator: string;
  numerator: string;
  unit: string;
};
export type UpdatePurchaseItemPrintedUnitPriceInput = {
  amountYen: any;
  basis: UpdatePurchaseItemQuantityInput;
};
export type ProductLearningConfigurationInput = {
  itemOnly: boolean;
  merchantAlias?: never;
  variantName?: never;
} | {
  itemOnly?: never;
  merchantAlias?: never;
  variantName: string;
} | {
  itemOnly?: never;
  merchantAlias: boolean;
  variantName?: never;
};
export type UpdatePurchaseItemAllocationInput = {
  poolId: string;
  weight: any;
};
export type UpdatePurchaseItemMutation$variables = {
  input: UpdatePurchaseItemInput;
};
export type UpdatePurchaseItemMutation$data = {
  readonly updatePurchaseItem: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly allocations: ReadonlyArray<{
        readonly amountYen: any;
        readonly poolId: string;
        readonly poolName: string;
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
    };
  };
};
export type UpdatePurchaseItemMutation = {
  response: UpdatePurchaseItemMutation$data;
  variables: UpdatePurchaseItemMutation$variables;
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
    "name": "updatePurchaseItem",
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
            "name": "result",
            "plural": false,
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
                "concreteType": "PurchaseItemAllocation",
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
        "type": "UpdatePurchaseItemSuccess",
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
    "name": "UpdatePurchaseItemMutation",
    "selections": (v7/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "UpdatePurchaseItemMutation",
    "selections": (v7/*:: as any*/)
  },
  "params": {
    "cacheID": "3c09af34ce32e7f67076678ed88c413e",
    "id": null,
    "metadata": {},
    "name": "UpdatePurchaseItemMutation",
    "operationKind": "mutation",
    "text": "mutation UpdatePurchaseItemMutation(\n  $input: UpdatePurchaseItemInput!\n) {\n  updatePurchaseItem(input: $input) {\n    __typename\n    ... on UpdatePurchaseItemSuccess {\n      result {\n        id\n        name\n        amountYen\n        currency\n        quantity {\n          numerator\n          denominator\n          unit\n        }\n        printedUnitPrice {\n          amountYen\n          basis {\n            numerator\n            denominator\n            unit\n          }\n        }\n        effectiveUnitPrice {\n          numeratorYen\n          denominator\n        }\n        revision\n        categoryId\n        productVariantId\n        allocations {\n          poolId\n          poolName\n          weight\n          amountYen\n        }\n        tags {\n          id\n          householdId\n          name\n          archived\n          revision\n        }\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "dcd7f9f1ab639bd272ce2c4ab283de9f";

export default node;

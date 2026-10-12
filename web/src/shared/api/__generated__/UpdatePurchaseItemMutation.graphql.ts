/**
 * @generated SignedSource<<5a599a2da6ce48515741c3811b40e982>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type ProductLearningScope = "ITEM_ONLY" | "MERCHANT_ALIAS" | "VARIANT_NAME" | "%future added value";
export type UpdatePurchaseItemInput = {
  allocations?: ReadonlyArray<AllocationInput> | null | undefined;
  amountYen?: any | null | undefined;
  baseRevision: number;
  categoryId?: string | null | undefined;
  changedFields: ReadonlyArray<string>;
  itemId: string;
  name?: string | null | undefined;
  printedUnitPriceQuantityDenominator?: string | null | undefined;
  printedUnitPriceQuantityNumerator?: string | null | undefined;
  printedUnitPriceUnit?: string | null | undefined;
  printedUnitPriceYen?: any | null | undefined;
  productLearning?: ProductLearningScope | null | undefined;
  productVariantId?: string | null | undefined;
  quantityDenominator?: string | null | undefined;
  quantityNumerator?: string | null | undefined;
  quantityUnit?: string | null | undefined;
  variantName?: string | null | undefined;
};
export type AllocationInput = {
  poolId: string;
  weight: any;
};
export type UpdatePurchaseItemMutation$variables = {
  input: UpdatePurchaseItemInput;
};
export type UpdatePurchaseItemMutation$data = {
  readonly updatePurchaseItem: {
    readonly amountYen: any;
    readonly categoryId: string | null | undefined;
    readonly effectiveUnitPriceDenominator: string | null | undefined;
    readonly effectiveUnitPriceNumeratorYen: string | null | undefined;
    readonly id: string;
    readonly name: string;
    readonly printedUnitPriceQuantityDenominator: string | null | undefined;
    readonly printedUnitPriceQuantityNumerator: string | null | undefined;
    readonly printedUnitPriceUnit: string | null | undefined;
    readonly printedUnitPriceYen: any | null | undefined;
    readonly quantityDenominator: string | null | undefined;
    readonly quantityNumerator: string | null | undefined;
    readonly quantityUnit: string | null | undefined;
    readonly revision: number;
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
    "concreteType": "PurchaseItem",
    "kind": "LinkedField",
    "name": "updatePurchaseItem",
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
        "name": "revision",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "name",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "amountYen",
        "storageKey": null
      },
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
        "name": "quantityNumerator",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "quantityDenominator",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "quantityUnit",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "printedUnitPriceYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "printedUnitPriceQuantityNumerator",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "printedUnitPriceQuantityDenominator",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "printedUnitPriceUnit",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "effectiveUnitPriceNumeratorYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "effectiveUnitPriceDenominator",
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
    "name": "UpdatePurchaseItemMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "UpdatePurchaseItemMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "ddb9b765c0c6e99fade77588fb3ab280",
    "id": null,
    "metadata": {},
    "name": "UpdatePurchaseItemMutation",
    "operationKind": "mutation",
    "text": "mutation UpdatePurchaseItemMutation(\n  $input: UpdatePurchaseItemInput!\n) {\n  updatePurchaseItem(input: $input) {\n    id\n    revision\n    name\n    amountYen\n    categoryId\n    quantityNumerator\n    quantityDenominator\n    quantityUnit\n    printedUnitPriceYen\n    printedUnitPriceQuantityNumerator\n    printedUnitPriceQuantityDenominator\n    printedUnitPriceUnit\n    effectiveUnitPriceNumeratorYen\n    effectiveUnitPriceDenominator\n  }\n}\n"
  }
};
})();

(node as any).hash = "d6667d6bbb123ddc33c60ed71fa7c8f4";

export default node;

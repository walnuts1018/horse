/**
 * @generated SignedSource<<244498ea83437840a585569b6c5a969c>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type PurchaseItemFields$data = {
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
  readonly " $fragmentType": "PurchaseItemFields";
};
export type PurchaseItemFields$key = {
  readonly " $data"?: PurchaseItemFields$data;
  readonly " $fragmentSpreads": FragmentRefs<"PurchaseItemFields">;
};

const node: ReaderFragment = (function(){
var v0 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v1 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "denominator",
  "storageKey": null
},
v4 = [
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "numerator",
    "storageKey": null
  },
  (v3/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "unit",
    "storageKey": null
  }
],
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
};
return {
  "argumentDefinitions": [],
  "kind": "Fragment",
  "metadata": null,
  "name": "PurchaseItemFields",
  "selections": [
    (v0/*:: as any*/),
    (v1/*:: as any*/),
    (v2/*:: as any*/),
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
      "selections": (v4/*:: as any*/),
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
        (v2/*:: as any*/),
        {
          "alias": null,
          "args": null,
          "concreteType": "PurchaseQuantity",
          "kind": "LinkedField",
          "name": "basis",
          "plural": false,
          "selections": (v4/*:: as any*/),
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
        (v3/*:: as any*/)
      ],
      "storageKey": null
    },
    (v5/*:: as any*/),
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
        (v2/*:: as any*/)
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
        (v0/*:: as any*/),
        {
          "alias": null,
          "args": null,
          "kind": "ScalarField",
          "name": "householdId",
          "storageKey": null
        },
        (v1/*:: as any*/),
        {
          "alias": null,
          "args": null,
          "kind": "ScalarField",
          "name": "archived",
          "storageKey": null
        },
        (v5/*:: as any*/)
      ],
      "storageKey": null
    }
  ],
  "type": "PurchaseItem",
  "abstractKey": null
};
})();

(node as any).hash = "db3018754608d10d9262ab843fbd3ed8";

export default node;

/**
 * @generated SignedSource<<a743ebab8a8203b681966525a6f21d1c>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseChangeEntity = "PURCHASE" | "PURCHASE_ITEM" | "%future added value";
export type PurchaseChangeField = "ALLOCATIONS" | "AMOUNT_YEN" | "CATEGORY_ID" | "CORRECTION" | "DELETED" | "MERCHANT" | "NAME" | "OCCURRED_AT" | "PAYMENT_ALLOCATIONS" | "PRINTED_UNIT_PRICE" | "PRODUCT_VARIANT_ID" | "QUANTITY" | "STATUS" | "TAGS" | "%future added value";
export type PurchaseChangeKind = "CORRECTED" | "CREATED" | "ITEM_DELETED" | "ITEM_UPDATED" | "POSTED" | "UPDATED" | "%future added value";
export type PurchaseDatePrecision = "DATE_ONLY" | "DATE_TIME" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
export type PurchaseChangesSubscription$variables = {
  after?: string | null | undefined;
  householdId: string;
};
export type PurchaseChangesSubscription$data = {
  readonly purchaseChanges: {
    readonly changedFields: ReadonlyArray<PurchaseChangeField>;
    readonly cursor: string;
    readonly entityId: string;
    readonly entityKind: PurchaseChangeEntity;
    readonly kind: PurchaseChangeKind;
    readonly purchase: {
      readonly correctionAmountYen: any;
      readonly correctionCount: number;
      readonly createdAt: string;
      readonly datePrecision: PurchaseDatePrecision;
      readonly hasEvidence: boolean;
      readonly householdId: string;
      readonly id: string;
      readonly items: ReadonlyArray<{
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
        readonly remainingRefundableAmountYen: any;
        readonly revision: number;
        readonly tags: ReadonlyArray<{
          readonly archived: boolean;
          readonly householdId: string;
          readonly id: string;
          readonly name: string;
          readonly revision: number;
        }>;
      }>;
      readonly merchant: string;
      readonly occurredAt: string;
      readonly origin: PurchaseOrigin;
      readonly paymentAllocations: ReadonlyArray<{
        readonly account: {
          readonly id: string;
          readonly name: string;
        };
        readonly amountYen: any;
        readonly paymentAccountId: string;
      }>;
      readonly remainingRefundableAmountYen: any;
      readonly revision: number;
      readonly sourceKinds: ReadonlyArray<SourceKind>;
      readonly status: PurchaseStatus;
      readonly totalYen: any;
      readonly updatedAt: string;
    };
  };
};
export type PurchaseChangesSubscription = {
  response: PurchaseChangesSubscription$data;
  variables: PurchaseChangesSubscription$variables;
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
  "name": "householdId"
},
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
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
  "name": "remainingRefundableAmountYen",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "denominator",
  "storageKey": null
},
v8 = [
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "numerator",
    "storageKey": null
  },
  (v7/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "unit",
    "storageKey": null
  }
],
v9 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v10 = [
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
        "name": "householdId",
        "variableName": "householdId"
      }
    ],
    "concreteType": "PurchaseChange",
    "kind": "LinkedField",
    "name": "purchaseChanges",
    "plural": false,
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
        "kind": "ScalarField",
        "name": "entityId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "entityKind",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "kind",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "changedFields",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "Purchase",
        "kind": "LinkedField",
        "name": "purchase",
        "plural": false,
        "selections": [
          (v2/*:: as any*/),
          (v3/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "occurredAt",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "datePrecision",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "merchant",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "origin",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "hasEvidence",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "sourceKinds",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "PurchasePayment",
            "kind": "LinkedField",
            "name": "paymentAllocations",
            "plural": true,
            "selections": [
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "paymentAccountId",
                "storageKey": null
              },
              (v4/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": "Account",
                "kind": "LinkedField",
                "name": "account",
                "plural": false,
                "selections": [
                  (v2/*:: as any*/),
                  (v5/*:: as any*/)
                ],
                "storageKey": null
              }
            ],
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "PurchaseItem",
            "kind": "LinkedField",
            "name": "items",
            "plural": true,
            "selections": [
              (v2/*:: as any*/),
              (v5/*:: as any*/),
              (v4/*:: as any*/),
              (v6/*:: as any*/),
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
                "selections": (v8/*:: as any*/),
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
                  (v4/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": "PurchaseQuantity",
                    "kind": "LinkedField",
                    "name": "basis",
                    "plural": false,
                    "selections": (v8/*:: as any*/),
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
                  (v7/*:: as any*/)
                ],
                "storageKey": null
              },
              (v9/*:: as any*/),
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
                  (v4/*:: as any*/)
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
                  (v2/*:: as any*/),
                  (v3/*:: as any*/),
                  (v5/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "archived",
                    "storageKey": null
                  },
                  (v9/*:: as any*/)
                ],
                "storageKey": null
              }
            ],
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "totalYen",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "correctionAmountYen",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "correctionCount",
            "storageKey": null
          },
          (v6/*:: as any*/),
          (v9/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "status",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "createdAt",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "updatedAt",
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
      (v1/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "PurchaseChangesSubscription",
    "selections": (v10/*:: as any*/),
    "type": "Subscription",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "PurchaseChangesSubscription",
    "selections": (v10/*:: as any*/)
  },
  "params": {
    "cacheID": "0439b2c26c87fe93e300b77eed8f05eb",
    "id": null,
    "metadata": {},
    "name": "PurchaseChangesSubscription",
    "operationKind": "subscription",
    "text": "subscription PurchaseChangesSubscription(\n  $householdId: ID!\n  $after: String\n) {\n  purchaseChanges(householdId: $householdId, after: $after) {\n    cursor\n    entityId\n    entityKind\n    kind\n    changedFields\n    purchase {\n      id\n      householdId\n      occurredAt\n      datePrecision\n      merchant\n      origin\n      hasEvidence\n      sourceKinds\n      paymentAllocations {\n        paymentAccountId\n        amountYen\n        account {\n          id\n          name\n        }\n      }\n      items {\n        id\n        name\n        amountYen\n        remainingRefundableAmountYen\n        currency\n        quantity {\n          numerator\n          denominator\n          unit\n        }\n        printedUnitPrice {\n          amountYen\n          basis {\n            numerator\n            denominator\n            unit\n          }\n        }\n        effectiveUnitPrice {\n          numeratorYen\n          denominator\n        }\n        revision\n        categoryId\n        productVariantId\n        allocations {\n          poolId\n          poolName\n          weight\n          amountYen\n        }\n        tags {\n          id\n          householdId\n          name\n          archived\n          revision\n        }\n      }\n      totalYen\n      correctionAmountYen\n      correctionCount\n      remainingRefundableAmountYen\n      revision\n      status\n      createdAt\n      updatedAt\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "336f38a15a50b8c5a17b6ce8b93ede7d";

export default node;

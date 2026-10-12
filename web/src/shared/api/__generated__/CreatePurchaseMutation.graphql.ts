/**
 * @generated SignedSource<<cf1931003b9b0f994955c397304f1569>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type CreatePurchaseInput = {
  householdId: string;
  idempotencyKey: string;
  items: ReadonlyArray<PurchaseItemInput>;
  manualAmountOverrideReason?: string | null | undefined;
  merchant: string;
  occurredAt: string;
  origin?: PurchaseOrigin | null | undefined;
  paymentAllocations?: ReadonlyArray<PurchasePaymentInput> | null | undefined;
  receiptCandidateId?: string | null | undefined;
  sourceDocumentId?: string | null | undefined;
  sourceReference?: string | null | undefined;
  status?: PurchaseStatus | null | undefined;
};
export type PurchasePaymentInput = {
  amountYen: any;
  paymentAccountId: string;
};
export type PurchaseItemInput = {
  allocations?: ReadonlyArray<AllocationInput> | null | undefined;
  amountYen: any;
  categoryId?: string | null | undefined;
  name: string;
  printedUnitPrice?: PrintedUnitPriceInput | null | undefined;
  productVariantId?: string | null | undefined;
  quantity?: PurchaseQuantityInput | null | undefined;
};
export type PurchaseQuantityInput = {
  denominator: string;
  numerator: string;
  unit: string;
};
export type PrintedUnitPriceInput = {
  amountYen: any;
  basis: PurchaseQuantityInput;
};
export type AllocationInput = {
  poolId: string;
  weight: any;
};
export type CreatePurchaseMutation$variables = {
  input: CreatePurchaseInput;
};
export type CreatePurchaseMutation$data = {
  readonly createPurchase: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly householdId: string;
      readonly id: string;
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
      readonly merchant: string;
      readonly occurredAt: string;
      readonly revision: number;
      readonly status: PurchaseStatus;
      readonly totalYen: any;
    };
  };
};
export type CreatePurchaseMutation = {
  response: CreatePurchaseMutation$data;
  variables: CreatePurchaseMutation$variables;
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
  "name": "householdId",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "denominator",
  "storageKey": null
},
v7 = [
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "numerator",
    "storageKey": null
  },
  (v6/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "unit",
    "storageKey": null
  }
],
v8 = [
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
    "name": "createPurchase",
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
            "concreteType": "Purchase",
            "kind": "LinkedField",
            "name": "result",
            "plural": false,
            "selections": [
              (v1/*:: as any*/),
              (v2/*:: as any*/),
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
                "name": "merchant",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "totalYen",
                "storageKey": null
              },
              (v3/*:: as any*/),
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
                "concreteType": "PurchaseItem",
                "kind": "LinkedField",
                "name": "items",
                "plural": true,
                "selections": [
                  (v1/*:: as any*/),
                  (v4/*:: as any*/),
                  (v5/*:: as any*/),
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
                    "selections": (v7/*:: as any*/),
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
                      (v5/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "concreteType": "PurchaseQuantity",
                        "kind": "LinkedField",
                        "name": "basis",
                        "plural": false,
                        "selections": (v7/*:: as any*/),
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
                      (v6/*:: as any*/)
                    ],
                    "storageKey": null
                  },
                  (v3/*:: as any*/),
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
                      (v5/*:: as any*/)
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
                      (v2/*:: as any*/),
                      (v4/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "archived",
                        "storageKey": null
                      },
                      (v3/*:: as any*/)
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
        "type": "CreatePurchaseSuccess",
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
    "name": "CreatePurchaseMutation",
    "selections": (v8/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreatePurchaseMutation",
    "selections": (v8/*:: as any*/)
  },
  "params": {
    "cacheID": "55591052ae2b127c909c6e2e90f40f67",
    "id": null,
    "metadata": {},
    "name": "CreatePurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation CreatePurchaseMutation(\n  $input: CreatePurchaseInput!\n) {\n  createPurchase(input: $input) {\n    __typename\n    ... on CreatePurchaseSuccess {\n      result {\n        id\n        householdId\n        occurredAt\n        merchant\n        totalYen\n        revision\n        status\n        items {\n          id\n          name\n          amountYen\n          currency\n          quantity {\n            numerator\n            denominator\n            unit\n          }\n          printedUnitPrice {\n            amountYen\n            basis {\n              numerator\n              denominator\n              unit\n            }\n          }\n          effectiveUnitPrice {\n            numeratorYen\n            denominator\n          }\n          revision\n          categoryId\n          productVariantId\n          allocations {\n            poolId\n            poolName\n            weight\n            amountYen\n          }\n          tags {\n            id\n            householdId\n            name\n            archived\n            revision\n          }\n        }\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "c422b11c0d89da5d47e7360b5c318673";

export default node;

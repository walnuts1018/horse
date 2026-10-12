/**
 * @generated SignedSource<<99be9d39706b25b4f530672c698ab718>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type CreatePurchaseInput = {
  householdId: string;
  idempotencyKey: string;
  items: ReadonlyArray<PurchaseItemInput>;
  merchant: string;
  occurredAt: string;
  paymentAccountId?: string | null | undefined;
  paymentAllocations?: ReadonlyArray<PurchasePaymentInput> | null | undefined;
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
  productVariantId?: string | null | undefined;
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
    readonly householdId: string;
    readonly id: string;
    readonly items: ReadonlyArray<{
      readonly allocations: ReadonlyArray<{
        readonly amountYen: any | null | undefined;
        readonly poolId: string;
        readonly weight: any;
      }>;
      readonly amountYen: any;
      readonly categoryId: string | null | undefined;
      readonly id: string;
      readonly name: string;
      readonly productVariantId: string | null | undefined;
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
    readonly paymentAccountId: string | null | undefined;
    readonly revision: number;
    readonly status: PurchaseStatus;
    readonly totalYen: any;
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
v6 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "input",
        "variableName": "input"
      }
    ],
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "createPurchase",
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
        "name": "paymentAccountId",
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
            "concreteType": "Tag",
            "kind": "LinkedField",
            "name": "tags",
            "plural": true,
            "selections": [
              (v1/*:: as any*/),
              (v4/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "archived",
                "storageKey": null
              },
              (v3/*:: as any*/),
              (v2/*:: as any*/)
            ],
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
              },
              (v5/*:: as any*/)
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
    "name": "CreatePurchaseMutation",
    "selections": (v6/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreatePurchaseMutation",
    "selections": (v6/*:: as any*/)
  },
  "params": {
    "cacheID": "fc2c28360985933b7b6098178debc8a9",
    "id": null,
    "metadata": {},
    "name": "CreatePurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation CreatePurchaseMutation(\n  $input: CreatePurchaseInput!\n) {\n  createPurchase(input: $input) {\n    id\n    householdId\n    occurredAt\n    merchant\n    paymentAccountId\n    totalYen\n    revision\n    status\n    items {\n      id\n      name\n      amountYen\n      categoryId\n      productVariantId\n      tags {\n        id\n        name\n        archived\n        revision\n        householdId\n      }\n      allocations {\n        poolId\n        weight\n        amountYen\n      }\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "5ac61e50d48aee7fecbd44fa6370dee0";

export default node;

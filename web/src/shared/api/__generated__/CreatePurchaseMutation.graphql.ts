/**
 * @generated SignedSource<<96bd7e7a7151baeee7e71c724738cd91>>
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
  sourceDocumentId?: string | null | undefined;
  sourceReference?: string | null | undefined;
  status?: PurchaseStatus | null | undefined;
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
  "name": "amountYen",
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
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "createPurchase",
    "plural": false,
    "selections": [
      (v1/*:: as any*/),
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "householdId",
        "storageKey": null
      },
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
            "name": "categoryId",
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
              (v2/*:: as any*/)
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
    "selections": (v3/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreatePurchaseMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "f3b64bb426006ff7e78168b1db03f35c",
    "id": null,
    "metadata": {},
    "name": "CreatePurchaseMutation",
    "operationKind": "mutation",
    "text": "mutation CreatePurchaseMutation(\n  $input: CreatePurchaseInput!\n) {\n  createPurchase(input: $input) {\n    id\n    householdId\n    occurredAt\n    merchant\n    paymentAccountId\n    totalYen\n    revision\n    status\n    items {\n      id\n      name\n      amountYen\n      categoryId\n      allocations {\n        poolId\n        weight\n        amountYen\n      }\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "22a42d8213ef05c0ba08a8fb650f51a5";

export default node;

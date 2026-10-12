/**
 * @generated SignedSource<<e8aa97c15940be34350f86f13d5ef0b0>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type BulkUpdatePurchasesInput = {
  changedFields: ReadonlyArray<string>;
  targets: ReadonlyArray<PurchaseUpdateTargetInput>;
};
export type PurchaseUpdateTargetInput = {
  baseRevision: number;
  patch: BulkPurchasePatch;
  purchaseId: string;
};
export type BulkPurchasePatch = {
  merchant?: string | null | undefined;
  occurredAt?: string | null | undefined;
  paymentAllocations?: ReadonlyArray<PurchasePaymentInput> | null | undefined;
};
export type PurchasePaymentInput = {
  amountYen: any;
  paymentAccountId: string;
};
export type BulkUpdatePurchasesMutation$variables = {
  input: BulkUpdatePurchasesInput;
};
export type BulkUpdatePurchasesMutation$data = {
  readonly bulkUpdatePurchases: {
    readonly __typename: "BulkUpdatePurchasesSuccess";
    readonly purchases: ReadonlyArray<{
      readonly id: string;
      readonly revision: number;
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
export type BulkUpdatePurchasesMutation = {
  response: BulkUpdatePurchasesMutation$data;
  variables: BulkUpdatePurchasesMutation$variables;
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
    "concreteType": null,
    "kind": "LinkedField",
    "name": "bulkUpdatePurchases",
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
            "name": "purchases",
            "plural": true,
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
              }
            ],
            "storageKey": null
          }
        ],
        "type": "BulkUpdatePurchasesSuccess",
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
    "name": "BulkUpdatePurchasesMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "BulkUpdatePurchasesMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "2f8d1df4103fd3a41aa175dda256248d",
    "id": null,
    "metadata": {},
    "name": "BulkUpdatePurchasesMutation",
    "operationKind": "mutation",
    "text": "mutation BulkUpdatePurchasesMutation(\n  $input: BulkUpdatePurchasesInput!\n) {\n  bulkUpdatePurchases(input: $input) {\n    __typename\n    ... on BulkUpdatePurchasesSuccess {\n      purchases {\n        id\n        revision\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "36c9a5179ca00c654a606e3c09da3f2c";

export default node;

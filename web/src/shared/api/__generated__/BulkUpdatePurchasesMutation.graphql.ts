/**
 * @generated SignedSource<<37f25fc448c590872f0eb8ed5ccd13ea>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type PurchaseDatePrecision = "DATE_ONLY" | "DATE_TIME" | "%future added value";
export type BulkUpdatePurchasesInput = {
  targets: ReadonlyArray<PurchaseUpdateTargetInput>;
};
export type PurchaseUpdateTargetInput = {
  baseRevision: number;
  patch: BulkPurchasePatch;
  purchaseId: string;
};
export type BulkPurchasePatch = {
  datePrecision?: PurchaseDatePrecision | null | undefined;
  merchant?: string | null | undefined;
  occurredAt?: string | null | undefined;
  paymentAllocations?: ReadonlyArray<BulkUpdatePurchasePaymentInput> | null | undefined;
};
export type BulkUpdatePurchasePaymentInput = {
  amountYen: any;
  paymentAccountId: string;
};
export type BulkUpdatePurchasesMutation$variables = {
  input: BulkUpdatePurchasesInput;
};
export type BulkUpdatePurchasesMutation$data = {
  readonly bulkUpdatePurchases: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly purchases?: ReadonlyArray<{
      readonly id: string;
      readonly revision: number;
    }>;
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
    "cacheID": "88b275771907d81c77ff9ca8f0b2fdba",
    "id": null,
    "metadata": {},
    "name": "BulkUpdatePurchasesMutation",
    "operationKind": "mutation",
    "text": "mutation BulkUpdatePurchasesMutation(\n  $input: BulkUpdatePurchasesInput!\n) {\n  bulkUpdatePurchases(input: $input) {\n    __typename\n    ... on BulkUpdatePurchasesSuccess {\n      purchases {\n        id\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "36c9a5179ca00c654a606e3c09da3f2c";

export default node;

/**
 * @generated SignedSource<<fb70818cde3d6323c25188bb78c5bcbc>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type BulkDeletePurchaseItemsInput = {
  targets: ReadonlyArray<PurchaseItemTargetInput>;
};
export type PurchaseItemTargetInput = {
  baseRevision: number;
  itemId: string;
};
export type BulkDeletePurchaseItemsMutation$variables = {
  input: BulkDeletePurchaseItemsInput;
};
export type BulkDeletePurchaseItemsMutation$data = {
  readonly bulkDeletePurchaseItems: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly deletedItemIds?: ReadonlyArray<string>;
    readonly message?: string;
  };
};
export type BulkDeletePurchaseItemsMutation = {
  response: BulkDeletePurchaseItemsMutation$data;
  variables: BulkDeletePurchaseItemsMutation$variables;
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
    "name": "bulkDeletePurchaseItems",
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
            "kind": "ScalarField",
            "name": "deletedItemIds",
            "storageKey": null
          }
        ],
        "type": "BulkDeletePurchaseItemsSuccess",
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
    "name": "BulkDeletePurchaseItemsMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "BulkDeletePurchaseItemsMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "56aa519a3d6ed0e59ae34c357d6312f8",
    "id": null,
    "metadata": {},
    "name": "BulkDeletePurchaseItemsMutation",
    "operationKind": "mutation",
    "text": "mutation BulkDeletePurchaseItemsMutation(\n  $input: BulkDeletePurchaseItemsInput!\n) {\n  bulkDeletePurchaseItems(input: $input) {\n    __typename\n    ... on BulkDeletePurchaseItemsSuccess {\n      deletedItemIds\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "799511df6fbb0447e238d625e284fd70";

export default node;

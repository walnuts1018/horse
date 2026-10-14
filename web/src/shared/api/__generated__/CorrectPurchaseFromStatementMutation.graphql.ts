/**
 * @generated SignedSource<<4b110796a6cf90d6aaa0a943bfe317aa>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type CorrectPurchaseFromStatementInput = {
  householdId: string;
  idempotencyKey: string;
  reason: string;
  sourceDocumentId: string;
  sourceReference: string;
  target: CorrectPurchaseTargetInput;
};
export type CorrectPurchaseTargetInput = {
  item?: never;
  purchase: PurchaseCorrectionPurchaseTargetInput;
} | {
  item: PurchaseCorrectionItemTargetInput;
  purchase?: never;
};
export type PurchaseCorrectionPurchaseTargetInput = {
  purchaseId: string;
};
export type PurchaseCorrectionItemTargetInput = {
  itemId: string;
  purchaseId: string;
};
export type CorrectPurchaseFromStatementMutation$variables = {
  input: CorrectPurchaseFromStatementInput;
};
export type CorrectPurchaseFromStatementMutation$data = {
  readonly correctPurchaseFromStatement: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
      readonly revision: number;
    };
  };
};
export type CorrectPurchaseFromStatementMutation = {
  response: CorrectPurchaseFromStatementMutation$data;
  variables: CorrectPurchaseFromStatementMutation$variables;
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
    "name": "correctPurchaseFromStatement",
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
        "type": "CorrectPurchaseSuccess",
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
    "name": "CorrectPurchaseFromStatementMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CorrectPurchaseFromStatementMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "1b45915574d1085008ee577bcbdcd665",
    "id": null,
    "metadata": {},
    "name": "CorrectPurchaseFromStatementMutation",
    "operationKind": "mutation",
    "text": "mutation CorrectPurchaseFromStatementMutation(\n  $input: CorrectPurchaseFromStatementInput!\n) {\n  correctPurchaseFromStatement(input: $input) {\n    __typename\n    ... on CorrectPurchaseSuccess {\n      result {\n        id\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "0fdaebea6cc5f14deba3fd90bf7fdc25";

export default node;

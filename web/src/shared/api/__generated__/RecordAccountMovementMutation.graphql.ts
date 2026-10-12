/**
 * @generated SignedSource<<ddd72bd512ee0400349725a8ff7d3f3e>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountMovementKind = "CARD_BILL_PAYMENT" | "TRANSFER" | "%future added value";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type RecordAccountMovementInput = {
  amountYen: any;
  description: string;
  fromAccountId: string;
  householdId: string;
  idempotencyKey: string;
  kind: AccountMovementKind;
  occurredAt: string;
  toAccountId: string;
};
export type RecordAccountMovementMutation$variables = {
  input: RecordAccountMovementInput;
};
export type RecordAccountMovementMutation$data = {
  readonly recordAccountMovement: {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "RecordAccountMovementSuccess";
    readonly result: {
      readonly amountYen: any;
      readonly description: string;
      readonly fromAccountId: string;
      readonly id: string;
      readonly kind: AccountMovementKind;
      readonly occurredAt: string;
      readonly toAccountId: string;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type RecordAccountMovementMutation = {
  response: RecordAccountMovementMutation$data;
  variables: RecordAccountMovementMutation$variables;
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
    "name": "recordAccountMovement",
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
            "concreteType": "AccountMovement",
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
                "name": "kind",
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
                "name": "fromAccountId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "toAccountId",
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
                "name": "description",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "RecordAccountMovementSuccess",
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
    "name": "RecordAccountMovementMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecordAccountMovementMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "08e304fccbd3fa5f6d4bfdac9973da75",
    "id": null,
    "metadata": {},
    "name": "RecordAccountMovementMutation",
    "operationKind": "mutation",
    "text": "mutation RecordAccountMovementMutation(\n  $input: RecordAccountMovementInput!\n) {\n  recordAccountMovement(input: $input) {\n    __typename\n    ... on RecordAccountMovementSuccess {\n      result {\n        id\n        kind\n        occurredAt\n        fromAccountId\n        toAccountId\n        amountYen\n        description\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "f2b378a287d2d2f0df1f9ba8b5f686b8";

export default node;

/**
 * @generated SignedSource<<898077abd3f4fb10de58decfb900ee37>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type RecordExternalIncomeInput = {
  accountId: string;
  amountYen: any;
  description: string;
  householdId: string;
  idempotencyKey: string;
  occurredAt: string;
};
export type RecordExternalIncomeMutation$variables = {
  input: RecordExternalIncomeInput;
};
export type RecordExternalIncomeMutation$data = {
  readonly recordExternalIncome: {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "RecordExternalIncomeSuccess";
    readonly result: {
      readonly accountId: string;
      readonly amountYen: any;
      readonly currency: string;
      readonly description: string;
      readonly householdId: string;
      readonly id: string;
      readonly occurredAt: string;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type RecordExternalIncomeMutation = {
  response: RecordExternalIncomeMutation$data;
  variables: RecordExternalIncomeMutation$variables;
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
    "name": "recordExternalIncome",
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
            "concreteType": "AccountIncome",
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
                "name": "householdId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "accountId",
                "storageKey": null
              },
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
                "kind": "ScalarField",
                "name": "occurredAt",
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
        "type": "RecordExternalIncomeSuccess",
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
    "name": "RecordExternalIncomeMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecordExternalIncomeMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "59ce01330df93c2ef3855d733a7b5b8a",
    "id": null,
    "metadata": {},
    "name": "RecordExternalIncomeMutation",
    "operationKind": "mutation",
    "text": "mutation RecordExternalIncomeMutation(\n  $input: RecordExternalIncomeInput!\n) {\n  recordExternalIncome(input: $input) {\n    __typename\n    ... on RecordExternalIncomeSuccess {\n      result {\n        id\n        householdId\n        accountId\n        currency\n        occurredAt\n        amountYen\n        description\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "b2a4a4f3c0198801387da066a0553250";

export default node;

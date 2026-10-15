/**
 * @generated SignedSource<<553d6e7b0649bae9df568c1bde36a8cd>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type CreateSharedPurchaseDraftMutation$variables = {
  householdId: string;
  payload: string;
};
export type CreateSharedPurchaseDraftMutation$data = {
  readonly createSharedPurchaseDraft: {
    readonly __typename: "CreateSharedPurchaseDraftSuccess";
    readonly result: {
      readonly createdAt: string;
      readonly householdId: string;
      readonly id: string;
      readonly payload: string;
      readonly revision: number;
      readonly updatedAt: string;
    };
  } | {
    readonly __typename: "SharedPurchaseDraftUserError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type CreateSharedPurchaseDraftMutation = {
  response: CreateSharedPurchaseDraftMutation$data;
  variables: CreateSharedPurchaseDraftMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "payload"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "fields": [
          {
            "kind": "Variable",
            "name": "householdId",
            "variableName": "householdId"
          },
          {
            "kind": "Variable",
            "name": "payload",
            "variableName": "payload"
          }
        ],
        "kind": "ObjectValue",
        "name": "input"
      }
    ],
    "concreteType": null,
    "kind": "LinkedField",
    "name": "createSharedPurchaseDraft",
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
            "concreteType": "SharedPurchaseDraft",
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
                "name": "revision",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "payload",
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
        "type": "CreateSharedPurchaseDraftSuccess",
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
        "type": "SharedPurchaseDraftUserError",
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
    "name": "CreateSharedPurchaseDraftMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateSharedPurchaseDraftMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "16a1d418e65cdea31961d268819297a0",
    "id": null,
    "metadata": {},
    "name": "CreateSharedPurchaseDraftMutation",
    "operationKind": "mutation",
    "text": "mutation CreateSharedPurchaseDraftMutation(\n  $householdId: ID!\n  $payload: String!\n) {\n  createSharedPurchaseDraft(input: {householdId: $householdId, payload: $payload}) {\n    __typename\n    ... on CreateSharedPurchaseDraftSuccess {\n      result {\n        id\n        householdId\n        revision\n        payload\n        createdAt\n        updatedAt\n      }\n    }\n    ... on SharedPurchaseDraftUserError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "776376e1ed029666ff33a09870955df1";

export default node;

/**
 * @generated SignedSource<<a6057e713ebb33e9bf3809c240434126>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type UpdateSharedPurchaseDraftMutation$variables = {
  expectedRevision: number;
  id: string;
  payload: string;
};
export type UpdateSharedPurchaseDraftMutation$data = {
  readonly updateSharedPurchaseDraft: {
    readonly __typename: "SharedPurchaseDraftUserError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "UpdateSharedPurchaseDraftConflict";
    readonly code: MutationErrorCode;
    readonly draft: {
      readonly createdAt: string;
      readonly householdId: string;
      readonly id: string;
      readonly payload: string;
      readonly revision: number;
      readonly updatedAt: string;
    };
    readonly message: string;
  } | {
    readonly __typename: "UpdateSharedPurchaseDraftSuccess";
    readonly result: {
      readonly createdAt: string;
      readonly householdId: string;
      readonly id: string;
      readonly payload: string;
      readonly revision: number;
      readonly updatedAt: string;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type UpdateSharedPurchaseDraftMutation = {
  response: UpdateSharedPurchaseDraftMutation$data;
  variables: UpdateSharedPurchaseDraftMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "expectedRevision"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "id"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "payload"
},
v3 = [
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
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "code",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "message",
  "storageKey": null
},
v6 = [
  {
    "alias": null,
    "args": [
      {
        "fields": [
          {
            "kind": "Variable",
            "name": "expectedRevision",
            "variableName": "expectedRevision"
          },
          {
            "kind": "Variable",
            "name": "id",
            "variableName": "id"
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
    "name": "updateSharedPurchaseDraft",
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
            "selections": (v3/*:: as any*/),
            "storageKey": null
          }
        ],
        "type": "UpdateSharedPurchaseDraftSuccess",
        "abstractKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          (v4/*:: as any*/),
          (v5/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "concreteType": "SharedPurchaseDraft",
            "kind": "LinkedField",
            "name": "draft",
            "plural": false,
            "selections": (v3/*:: as any*/),
            "storageKey": null
          }
        ],
        "type": "UpdateSharedPurchaseDraftConflict",
        "abstractKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          (v4/*:: as any*/),
          (v5/*:: as any*/)
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
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "UpdateSharedPurchaseDraftMutation",
    "selections": (v6/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "UpdateSharedPurchaseDraftMutation",
    "selections": (v6/*:: as any*/)
  },
  "params": {
    "cacheID": "3d2086612df72d12b8ea55726495ff32",
    "id": null,
    "metadata": {},
    "name": "UpdateSharedPurchaseDraftMutation",
    "operationKind": "mutation",
    "text": "mutation UpdateSharedPurchaseDraftMutation(\n  $id: ID!\n  $expectedRevision: Int!\n  $payload: String!\n) {\n  updateSharedPurchaseDraft(input: {id: $id, expectedRevision: $expectedRevision, payload: $payload}) {\n    __typename\n    ... on UpdateSharedPurchaseDraftSuccess {\n      result {\n        id\n        householdId\n        revision\n        payload\n        createdAt\n        updatedAt\n      }\n    }\n    ... on UpdateSharedPurchaseDraftConflict {\n      code\n      message\n      draft {\n        id\n        householdId\n        revision\n        payload\n        createdAt\n        updatedAt\n      }\n    }\n    ... on SharedPurchaseDraftUserError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "a07694cceb03c72df6d72fa05e6cf765";

export default node;

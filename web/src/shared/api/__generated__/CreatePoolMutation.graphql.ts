/**
 * @generated SignedSource<<64f67cd3f79e6e36ac0775868510b52f>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type PoolKind = "PERSONAL" | "SHARED" | "%future added value";
export type CreatePoolInput = {
  householdId: string;
  kind: PoolKind;
  name: string;
};
export type CreatePoolMutation$variables = {
  input: CreatePoolInput;
};
export type CreatePoolMutation$data = {
  readonly createPool: {
    readonly __typename: "CreatePoolSuccess";
    readonly result: {
      readonly id: string;
    };
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
export type CreatePoolMutation = {
  response: CreatePoolMutation$data;
  variables: CreatePoolMutation$variables;
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
    "name": "createPool",
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
            "concreteType": "Pool",
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
              }
            ],
            "storageKey": null
          }
        ],
        "type": "CreatePoolSuccess",
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
    "name": "CreatePoolMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreatePoolMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "7aae90bf3012810cd9f44f01f214e9ff",
    "id": null,
    "metadata": {},
    "name": "CreatePoolMutation",
    "operationKind": "mutation",
    "text": "mutation CreatePoolMutation(\n  $input: CreatePoolInput!\n) {\n  createPool(input: $input) {\n    __typename\n    ... on CreatePoolSuccess {\n      result {\n        id\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "95d575d97c91b355aed7921f1cb387c5";

export default node;

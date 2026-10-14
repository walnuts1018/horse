/**
 * @generated SignedSource<<b301286593ffd6dc46d3a7f6b5ac4eed>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type SetAccountCardLast4Input = {
  accountId: string;
  cardLast4?: string | null | undefined;
};
export type SetAccountCardLast4Mutation$variables = {
  input: SetAccountCardLast4Input;
};
export type SetAccountCardLast4Mutation$data = {
  readonly setAccountCardLast4: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly cardLast4: string | null | undefined;
      readonly id: string;
    };
  };
};
export type SetAccountCardLast4Mutation = {
  response: SetAccountCardLast4Mutation$data;
  variables: SetAccountCardLast4Mutation$variables;
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
    "name": "setAccountCardLast4",
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
            "concreteType": "Account",
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
                "name": "cardLast4",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "SetAccountCardLast4Success",
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
    "name": "SetAccountCardLast4Mutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetAccountCardLast4Mutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "24eeb9ffb9737b591964ac09f04392ce",
    "id": null,
    "metadata": {},
    "name": "SetAccountCardLast4Mutation",
    "operationKind": "mutation",
    "text": "mutation SetAccountCardLast4Mutation(\n  $input: SetAccountCardLast4Input!\n) {\n  setAccountCardLast4(input: $input) {\n    __typename\n    ... on SetAccountCardLast4Success {\n      result {\n        id\n        cardLast4\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "758520780f501cd293485dfe3bea2354";

export default node;

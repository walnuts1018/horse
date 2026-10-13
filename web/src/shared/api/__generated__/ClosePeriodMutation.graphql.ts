/**
 * @generated SignedSource<<dec721a68b1abc108586b683afcc6e0e>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type ClosePeriodInput = {
  expectedRevision: number;
  householdId: string;
  period: string;
};
export type ClosePeriodMutation$variables = {
  input: ClosePeriodInput;
};
export type ClosePeriodMutation$data = {
  readonly closePeriod: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly __typename: string;
      readonly createdAt?: string;
      readonly id: string;
      readonly period: string;
      readonly revision: number;
    };
  };
};
export type ClosePeriodMutation = {
  response: ClosePeriodMutation$data;
  variables: ClosePeriodMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "input"
  }
],
v1 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v2 = [
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
    "name": "closePeriod",
    "plural": false,
    "selections": [
      (v1/*:: as any*/),
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": null,
            "kind": "LinkedField",
            "name": "result",
            "plural": false,
            "selections": [
              (v1/*:: as any*/),
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
                "name": "period",
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
                "kind": "InlineFragment",
                "selections": [
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "createdAt",
                    "storageKey": null
                  }
                ],
                "type": "ClosedSettlement",
                "abstractKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "ClosePeriodSuccess",
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
    "name": "ClosePeriodMutation",
    "selections": (v2/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "ClosePeriodMutation",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "824aefa1a4ad1b70c21f19dda5a8338c",
    "id": null,
    "metadata": {},
    "name": "ClosePeriodMutation",
    "operationKind": "mutation",
    "text": "mutation ClosePeriodMutation(\n  $input: ClosePeriodInput!\n) {\n  closePeriod(input: $input) {\n    __typename\n    ... on ClosePeriodSuccess {\n      result {\n        __typename\n        id\n        period\n        revision\n        ... on ClosedSettlement {\n          createdAt\n        }\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "5a172ff2b9fd95097983f7436ff37aab";

export default node;

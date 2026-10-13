/**
 * @generated SignedSource<<e73339ac2fddb1f359f4fc65c738fe2f>>
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
export type SettlementClosePeriodMutation$variables = {
  input: ClosePeriodInput;
};
export type SettlementClosePeriodMutation$data = {
  readonly closePeriod: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly __typename: string;
      readonly id: string;
      readonly period: string;
      readonly revision: number;
    };
  };
};
export type SettlementClosePeriodMutation = {
  response: SettlementClosePeriodMutation$data;
  variables: SettlementClosePeriodMutation$variables;
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
    "name": "SettlementClosePeriodMutation",
    "selections": (v2/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SettlementClosePeriodMutation",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "3c431219fcbc93ad037364578251deaa",
    "id": null,
    "metadata": {},
    "name": "SettlementClosePeriodMutation",
    "operationKind": "mutation",
    "text": "mutation SettlementClosePeriodMutation(\n  $input: ClosePeriodInput!\n) {\n  closePeriod(input: $input) {\n    __typename\n    ... on ClosePeriodSuccess {\n      result {\n        __typename\n        id\n        period\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "3b08906b095a03da309568e7b5e7642a";

export default node;

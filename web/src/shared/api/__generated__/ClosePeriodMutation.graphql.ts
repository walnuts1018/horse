/**
 * @generated SignedSource<<5fd69d93a79b800041b40470d07febe2>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SettlementStatus = "CLOSED" | "OPEN" | "%future added value";
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
    readonly __typename: "ClosePeriodSuccess";
    readonly result: {
      readonly period: string;
      readonly revision: number;
      readonly status: SettlementStatus;
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
    "name": "closePeriod",
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
            "concreteType": "Settlement",
            "kind": "LinkedField",
            "name": "result",
            "plural": false,
            "selections": [
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
                "name": "status",
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
    "name": "ClosePeriodMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "ClosePeriodMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "eda53cb6dd3f260de79fd55a8f0082ba",
    "id": null,
    "metadata": {},
    "name": "ClosePeriodMutation",
    "operationKind": "mutation",
    "text": "mutation ClosePeriodMutation(\n  $input: ClosePeriodInput!\n) {\n  closePeriod(input: $input) {\n    __typename\n    ... on ClosePeriodSuccess {\n      result {\n        period\n        status\n        revision\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "96956fe1555f7d2424302347cd87865a";

export default node;

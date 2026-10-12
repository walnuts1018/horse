/**
 * @generated SignedSource<<93805c76b7e583f422da3ce767804629>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SetDefaultAllocationInput = {
  allocations: ReadonlyArray<AllocationInput>;
  householdId: string;
};
export type AllocationInput = {
  poolId: string;
  weight: any;
};
export type SetDefaultAllocationMutation$variables = {
  input: SetDefaultAllocationInput;
};
export type SetDefaultAllocationMutation$data = {
  readonly setDefaultAllocation: {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "SetDefaultAllocationSuccess";
    readonly result: {
      readonly id: string;
      readonly revision: number;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type SetDefaultAllocationMutation = {
  response: SetDefaultAllocationMutation$data;
  variables: SetDefaultAllocationMutation$variables;
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
    "name": "setDefaultAllocation",
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
            "concreteType": "Household",
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
        "type": "SetDefaultAllocationSuccess",
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
    "name": "SetDefaultAllocationMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetDefaultAllocationMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "2aee9d091e54a8cab098418992e8f3d3",
    "id": null,
    "metadata": {},
    "name": "SetDefaultAllocationMutation",
    "operationKind": "mutation",
    "text": "mutation SetDefaultAllocationMutation(\n  $input: SetDefaultAllocationInput!\n) {\n  setDefaultAllocation(input: $input) {\n    __typename\n    ... on SetDefaultAllocationSuccess {\n      result {\n        id\n        revision\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "9372e55ca555e9e9477ef3b053829616";

export default node;

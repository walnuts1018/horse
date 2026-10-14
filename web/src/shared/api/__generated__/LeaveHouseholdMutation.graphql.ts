/**
 * @generated SignedSource<<17f78518daf427c9811b1be668cd65ed>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type LeaveHouseholdInput = {
  householdId: string;
};
export type LeaveHouseholdMutation$variables = {
  input: LeaveHouseholdInput;
};
export type LeaveHouseholdMutation$data = {
  readonly leaveHousehold: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly householdId?: string;
    readonly message?: string;
  };
};
export type LeaveHouseholdMutation = {
  response: LeaveHouseholdMutation$data;
  variables: LeaveHouseholdMutation$variables;
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
    "name": "leaveHousehold",
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
            "kind": "ScalarField",
            "name": "householdId",
            "storageKey": null
          }
        ],
        "type": "LeaveHouseholdSuccess",
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
    "name": "LeaveHouseholdMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "LeaveHouseholdMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "b40913ec7eddc374d3a7df6995fd6474",
    "id": null,
    "metadata": {},
    "name": "LeaveHouseholdMutation",
    "operationKind": "mutation",
    "text": "mutation LeaveHouseholdMutation(\n  $input: LeaveHouseholdInput!\n) {\n  leaveHousehold(input: $input) {\n    __typename\n    ... on LeaveHouseholdSuccess {\n      householdId\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "bb0b6733cf3051d5e23e0502a8417e05";

export default node;

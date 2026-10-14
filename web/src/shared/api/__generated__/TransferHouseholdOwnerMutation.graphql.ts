/**
 * @generated SignedSource<<7b7031cd60d01e2699c06df1003becc6>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type TransferHouseholdOwnerInput = {
  householdId: string;
  newOwnerId: string;
};
export type TransferHouseholdOwnerMutation$variables = {
  input: TransferHouseholdOwnerInput;
};
export type TransferHouseholdOwnerMutation$data = {
  readonly transferHouseholdOwner: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly householdId?: string;
    readonly message?: string;
  };
};
export type TransferHouseholdOwnerMutation = {
  response: TransferHouseholdOwnerMutation$data;
  variables: TransferHouseholdOwnerMutation$variables;
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
    "name": "transferHouseholdOwner",
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
        "type": "TransferHouseholdOwnerSuccess",
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
    "name": "TransferHouseholdOwnerMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "TransferHouseholdOwnerMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "3b527ac2f2392baaa0eea8a01f0becc1",
    "id": null,
    "metadata": {},
    "name": "TransferHouseholdOwnerMutation",
    "operationKind": "mutation",
    "text": "mutation TransferHouseholdOwnerMutation(\n  $input: TransferHouseholdOwnerInput!\n) {\n  transferHouseholdOwner(input: $input) {\n    __typename\n    ... on TransferHouseholdOwnerSuccess {\n      householdId\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "6a8d8eb85e63d10191e37ec5ca111989";

export default node;

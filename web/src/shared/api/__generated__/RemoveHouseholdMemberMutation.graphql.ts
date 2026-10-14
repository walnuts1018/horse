/**
 * @generated SignedSource<<873fb1dad90a8c9c24db7b5725d9f866>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type RemoveHouseholdMemberInput = {
  householdId: string;
  memberId: string;
};
export type RemoveHouseholdMemberMutation$variables = {
  input: RemoveHouseholdMemberInput;
};
export type RemoveHouseholdMemberMutation$data = {
  readonly removeHouseholdMember: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly householdId?: string;
    readonly message?: string;
  };
};
export type RemoveHouseholdMemberMutation = {
  response: RemoveHouseholdMemberMutation$data;
  variables: RemoveHouseholdMemberMutation$variables;
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
    "name": "removeHouseholdMember",
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
        "type": "RemoveHouseholdMemberSuccess",
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
    "name": "RemoveHouseholdMemberMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RemoveHouseholdMemberMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "14713f923636b95408bdc063dbb8bf86",
    "id": null,
    "metadata": {},
    "name": "RemoveHouseholdMemberMutation",
    "operationKind": "mutation",
    "text": "mutation RemoveHouseholdMemberMutation(\n  $input: RemoveHouseholdMemberInput!\n) {\n  removeHouseholdMember(input: $input) {\n    __typename\n    ... on RemoveHouseholdMemberSuccess {\n      householdId\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "8ed408e778679e4322271b04ee858376";

export default node;

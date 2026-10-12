/**
 * @generated SignedSource<<77af44d1a27a0dc7a34780cf5beb93e6>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SetFundingScheduleInput = {
  amountYen: any;
  cadence: string;
  endsOn?: string | null | undefined;
  householdId: string;
  memberId: string;
  poolId: string;
  startsOn: string;
};
export type SetFundingScheduleMutation$variables = {
  input: SetFundingScheduleInput;
};
export type SetFundingScheduleMutation$data = {
  readonly setFundingSchedule: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly success?: boolean;
  };
};
export type SetFundingScheduleMutation = {
  response: SetFundingScheduleMutation$data;
  variables: SetFundingScheduleMutation$variables;
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
    "name": "setFundingSchedule",
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
            "name": "success",
            "storageKey": null
          }
        ],
        "type": "SetFundingScheduleSuccess",
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
    "name": "SetFundingScheduleMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetFundingScheduleMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "78a4e52639d34fd65e56683d12dea22a",
    "id": null,
    "metadata": {},
    "name": "SetFundingScheduleMutation",
    "operationKind": "mutation",
    "text": "mutation SetFundingScheduleMutation(\n  $input: SetFundingScheduleInput!\n) {\n  setFundingSchedule(input: $input) {\n    __typename\n    ... on SetFundingScheduleSuccess {\n      success\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "81a90ae9e0f28be3ab908fc5ebedb8e8";

export default node;

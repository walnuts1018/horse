/**
 * @generated SignedSource<<9d2f635b92365cd22eb5ab942306055b>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type FundingScheduleCadence = "CUSTOM" | "MONTHLY" | "WEEKLY" | "%future added value";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type SetFundingScheduleInput = {
  amountYen: any;
  cadence: FundingScheduleCadence;
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
    readonly result?: {
      readonly active: boolean;
      readonly amountYen: any;
      readonly cadence: FundingScheduleCadence;
      readonly createdAt: string;
      readonly endsOn: string | null | undefined;
      readonly householdId: string;
      readonly id: string;
      readonly memberId: string;
      readonly poolId: string;
      readonly startsOn: string;
    };
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
            "concreteType": "FundingSchedule",
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
                "name": "householdId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "memberId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "poolId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "amountYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "cadence",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "startsOn",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "endsOn",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "active",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "createdAt",
                "storageKey": null
              }
            ],
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
    "cacheID": "d107001c50712f32e7195b363032f980",
    "id": null,
    "metadata": {},
    "name": "SetFundingScheduleMutation",
    "operationKind": "mutation",
    "text": "mutation SetFundingScheduleMutation(\n  $input: SetFundingScheduleInput!\n) {\n  setFundingSchedule(input: $input) {\n    __typename\n    ... on SetFundingScheduleSuccess {\n      result {\n        id\n        householdId\n        memberId\n        poolId\n        amountYen\n        cadence\n        startsOn\n        endsOn\n        active\n        createdAt\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "6125a0cf4ddc7cd93d561be50dd04de3";

export default node;

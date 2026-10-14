/**
 * @generated SignedSource<<720066c000cc711aead442b012819295>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type SharedPoolSettlementPolicy = "BALANCE_TO_STANDARD" | "REIMBURSE_PERSONAL" | "%future added value";
export type SetPoolFundingRuleInput = {
  configuration: PoolFundingRuleConfigurationInput;
  expectedVersion: number;
  householdId: string;
  poolId: string;
};
export type PoolFundingRuleConfigurationInput = {
  personal: PersonalPoolFundingRuleInput;
  shared?: never;
} | {
  personal?: never;
  shared: SharedPoolFundingRuleInput;
};
export type PersonalPoolFundingRuleInput = {
  ownerMemberId: string;
};
export type SharedPoolFundingRuleInput = {
  fundingShares: ReadonlyArray<PoolFundingShareInput>;
  settlementPolicy: SharedPoolSettlementPolicy;
};
export type PoolFundingShareInput = {
  memberId: string;
  weight: any;
};
export type SetPoolFundingRuleMutation$variables = {
  input: SetPoolFundingRuleInput;
};
export type SetPoolFundingRuleMutation$data = {
  readonly setPoolFundingRule: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly __typename: string;
      readonly fundingRuleVersion: number;
      readonly fundingShares: ReadonlyArray<{
        readonly memberId: string;
        readonly memberName: string;
        readonly weight: any;
      }>;
      readonly id: string;
      readonly ownerMemberId?: string;
      readonly settlementPolicy?: SharedPoolSettlementPolicy;
    };
  };
};
export type SetPoolFundingRuleMutation = {
  response: SetPoolFundingRuleMutation$data;
  variables: SetPoolFundingRuleMutation$variables;
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
    "name": "setPoolFundingRule",
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
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "id",
                "storageKey": null
              },
              (v1/*:: as any*/),
              {
                "kind": "InlineFragment",
                "selections": [
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "ownerMemberId",
                    "storageKey": null
                  }
                ],
                "type": "PersonalPool",
                "abstractKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "fundingRuleVersion",
                "storageKey": null
              },
              {
                "kind": "InlineFragment",
                "selections": [
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "settlementPolicy",
                    "storageKey": null
                  }
                ],
                "type": "SharedPool",
                "abstractKey": null
              },
              {
                "alias": null,
                "args": null,
                "concreteType": "PoolFundingShare",
                "kind": "LinkedField",
                "name": "fundingShares",
                "plural": true,
                "selections": [
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
                    "name": "memberName",
                    "storageKey": null
                  },
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "weight",
                    "storageKey": null
                  }
                ],
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "SetPoolFundingRuleSuccess",
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
    "name": "SetPoolFundingRuleMutation",
    "selections": (v2/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetPoolFundingRuleMutation",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "b7a254699932a24ac5e1e2312c0af421",
    "id": null,
    "metadata": {},
    "name": "SetPoolFundingRuleMutation",
    "operationKind": "mutation",
    "text": "mutation SetPoolFundingRuleMutation(\n  $input: SetPoolFundingRuleInput!\n) {\n  setPoolFundingRule(input: $input) {\n    __typename\n    ... on SetPoolFundingRuleSuccess {\n      result {\n        id\n        __typename\n        ... on PersonalPool {\n          ownerMemberId\n        }\n        fundingRuleVersion\n        ... on SharedPool {\n          settlementPolicy\n        }\n        fundingShares {\n          memberId\n          memberName\n          weight\n        }\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "0539612e66276b323cb7c729aa3b2441";

export default node;

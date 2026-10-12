/**
 * @generated SignedSource<<f7bc291ae4eb9ecedd678f6d2ce6351f>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type PoolKind = "PERSONAL" | "SHARED" | "%future added value";
export type SharedPoolSettlementPolicy = "BALANCE_TO_STANDARD" | "REIMBURSE_PERSONAL" | "%future added value";
export type SetPoolFundingRuleInput = {
  expectedVersion: number;
  fundingShares: ReadonlyArray<PoolFundingShareInput>;
  householdId: string;
  ownerMemberId?: string | null | undefined;
  poolId: string;
  settlementPolicy?: SharedPoolSettlementPolicy | null | undefined;
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
      readonly fundingRuleVersion: number;
      readonly fundingShares: ReadonlyArray<{
        readonly memberId: string;
        readonly memberName: string;
        readonly weight: any;
      }>;
      readonly id: string;
      readonly kind: PoolKind;
      readonly ownerMemberId: string | null | undefined;
      readonly settlementPolicy: SharedPoolSettlementPolicy | null | undefined;
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
    "name": "setPoolFundingRule",
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
            "concreteType": "Pool",
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
                "name": "kind",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "ownerMemberId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "fundingRuleVersion",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "settlementPolicy",
                "storageKey": null
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
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetPoolFundingRuleMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "b1149c3412047e9fae4606cead53ffd8",
    "id": null,
    "metadata": {},
    "name": "SetPoolFundingRuleMutation",
    "operationKind": "mutation",
    "text": "mutation SetPoolFundingRuleMutation(\n  $input: SetPoolFundingRuleInput!\n) {\n  setPoolFundingRule(input: $input) {\n    __typename\n    ... on SetPoolFundingRuleSuccess {\n      result {\n        id\n        kind\n        ownerMemberId\n        fundingRuleVersion\n        settlementPolicy\n        fundingShares {\n          memberId\n          memberName\n          weight\n        }\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "ae3c38169390325529246248ecd51981";

export default node;

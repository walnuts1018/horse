/**
 * @generated SignedSource<<cf93e4ebce4659db30aa278e67e63fb8>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountKind = "BANK" | "CARD" | "CASH" | "OTHER" | "%future added value";
export type MemberRole = "MEMBER" | "OWNER" | "%future added value";
export type SharedPoolSettlementPolicy = "BALANCE_TO_STANDARD" | "REIMBURSE_PERSONAL" | "%future added value";
export type HouseholdQuery$variables = {
  id: string;
};
export type HouseholdQuery$data = {
  readonly household: {
    readonly accounts: ReadonlyArray<{
      readonly cardLast4: string | null | undefined;
      readonly id: string;
      readonly kind: AccountKind;
      readonly name: string;
      readonly poolId: string | null | undefined;
    }>;
    readonly categories: ReadonlyArray<{
      readonly archived: boolean;
      readonly id: string;
      readonly name: string;
      readonly parentId: string | null | undefined;
    }>;
    readonly defaultAllocation: ReadonlyArray<{
      readonly poolId: string;
      readonly weight: any;
    }>;
    readonly id: string;
    readonly members: ReadonlyArray<{
      readonly displayName: string;
      readonly id: string;
      readonly role: MemberRole;
    }>;
    readonly name: string;
    readonly pools: ReadonlyArray<{
      readonly __typename: string;
      readonly defaultShareWeight: any;
      readonly fundingRuleVersion: number;
      readonly fundingShares: ReadonlyArray<{
        readonly memberId: string;
        readonly memberName: string;
        readonly weight: any;
      }>;
      readonly id: string;
      readonly name: string;
      readonly ownerMemberId?: string;
      readonly settlementPolicy?: SharedPoolSettlementPolicy;
    }>;
    readonly revision: number;
  } | null | undefined;
};
export type HouseholdQuery = {
  response: HouseholdQuery$data;
  variables: HouseholdQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "id"
  }
],
v1 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "weight",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "poolId",
  "storageKey": null
},
v5 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "id",
        "variableName": "id"
      }
    ],
    "concreteType": "Household",
    "kind": "LinkedField",
    "name": "household",
    "plural": false,
    "selections": [
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "revision",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "Member",
        "kind": "LinkedField",
        "name": "members",
        "plural": true,
        "selections": [
          (v1/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "displayName",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "role",
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": null,
        "kind": "LinkedField",
        "name": "pools",
        "plural": true,
        "selections": [
          (v1/*:: as any*/),
          (v2/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "defaultShareWeight",
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
              (v3/*:: as any*/)
            ],
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
                "name": "ownerMemberId",
                "storageKey": null
              }
            ],
            "type": "PersonalPool",
            "abstractKey": null
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
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "Account",
        "kind": "LinkedField",
        "name": "accounts",
        "plural": true,
        "selections": [
          (v1/*:: as any*/),
          (v2/*:: as any*/),
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
            "name": "cardLast4",
            "storageKey": null
          },
          (v4/*:: as any*/)
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "Category",
        "kind": "LinkedField",
        "name": "categories",
        "plural": true,
        "selections": [
          (v1/*:: as any*/),
          (v2/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "parentId",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "archived",
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "Allocation",
        "kind": "LinkedField",
        "name": "defaultAllocation",
        "plural": true,
        "selections": [
          (v4/*:: as any*/),
          (v3/*:: as any*/)
        ],
        "storageKey": null
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
    "name": "HouseholdQuery",
    "selections": (v5/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "HouseholdQuery",
    "selections": (v5/*:: as any*/)
  },
  "params": {
    "cacheID": "ae9a9f92e7d747fcc4f0c738d57a294b",
    "id": null,
    "metadata": {},
    "name": "HouseholdQuery",
    "operationKind": "query",
    "text": "query HouseholdQuery(\n  $id: ID!\n) {\n  household(id: $id) {\n    id\n    name\n    revision\n    members {\n      id\n      displayName\n      role\n    }\n    pools {\n      id\n      name\n      defaultShareWeight\n      fundingShares {\n        memberId\n        memberName\n        weight\n      }\n      fundingRuleVersion\n      __typename\n      ... on PersonalPool {\n        ownerMemberId\n      }\n      ... on SharedPool {\n        settlementPolicy\n      }\n    }\n    accounts {\n      id\n      name\n      kind\n      cardLast4\n      poolId\n    }\n    categories {\n      id\n      name\n      parentId\n      archived\n    }\n    defaultAllocation {\n      poolId\n      weight\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "c18eeccdd562bb72994ae3248eddeaed";

export default node;

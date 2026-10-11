/**
 * @generated SignedSource<<602a66573bb957c8f67c8fa4508a9eed>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountKind = "BANK" | "CARD" | "CASH" | "OTHER" | "%future added value";
export type PoolKind = "PERSONAL" | "SHARED" | "%future added value";
export type HouseholdQuery$variables = {
  id: string;
};
export type HouseholdQuery$data = {
  readonly household: {
    readonly accounts: ReadonlyArray<{
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
      readonly role: string;
    }>;
    readonly name: string;
    readonly pools: ReadonlyArray<{
      readonly defaultShareWeight: any;
      readonly id: string;
      readonly kind: PoolKind;
      readonly name: string;
    }>;
    readonly revision: number;
  };
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
  "name": "kind",
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
        "concreteType": "Pool",
        "kind": "LinkedField",
        "name": "pools",
        "plural": true,
        "selections": [
          (v1/*:: as any*/),
          (v2/*:: as any*/),
          (v3/*:: as any*/),
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "defaultShareWeight",
            "storageKey": null
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
          (v3/*:: as any*/),
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
    "cacheID": "a29cef33f5e05678fe998feb826bd7da",
    "id": null,
    "metadata": {},
    "name": "HouseholdQuery",
    "operationKind": "query",
    "text": "query HouseholdQuery(\n  $id: ID!\n) {\n  household(id: $id) {\n    id\n    name\n    revision\n    members {\n      id\n      displayName\n      role\n    }\n    pools {\n      id\n      name\n      kind\n      defaultShareWeight\n    }\n    accounts {\n      id\n      name\n      kind\n      poolId\n    }\n    categories {\n      id\n      name\n      parentId\n      archived\n    }\n    defaultAllocation {\n      poolId\n      weight\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "8f5e3fbb16b4ce22593b15a78c61649b";

export default node;

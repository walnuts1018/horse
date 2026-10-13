/**
 * @generated SignedSource<<9048390476fc6d32bdd7acb3eecb73ff>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AllocationRulesQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type AllocationRulesQuery$data = {
  readonly allocationRules: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly allocations: ReadonlyArray<{
          readonly amountYen: any | null | undefined;
          readonly poolId: string;
          readonly poolName: string;
          readonly weight: any;
        }>;
        readonly householdId: string;
        readonly id: string;
        readonly revision: number;
        readonly target: {
          readonly __typename: string;
          readonly name: string;
        };
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
    readonly totalCount: number;
  };
};
export type AllocationRulesQuery = {
  response: AllocationRulesQuery$data;
  variables: AllocationRulesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "after"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v3 = [
  {
    "kind": "Variable",
    "name": "after",
    "variableName": "after"
  },
  {
    "kind": "Variable",
    "name": "first",
    "variableName": "first"
  },
  {
    "kind": "Variable",
    "name": "householdId",
    "variableName": "householdId"
  }
],
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "cursor",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "householdId",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v9 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v10 = {
  "alias": null,
  "args": null,
  "concreteType": "Allocation",
  "kind": "LinkedField",
  "name": "allocations",
  "plural": true,
  "selections": [
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
      "name": "poolName",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "weight",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "amountYen",
      "storageKey": null
    }
  ],
  "storageKey": null
},
v11 = {
  "alias": null,
  "args": null,
  "concreteType": "PageInfo",
  "kind": "LinkedField",
  "name": "pageInfo",
  "plural": false,
  "selections": [
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "endCursor",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "hasNextPage",
      "storageKey": null
    }
  ],
  "storageKey": null
},
v12 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "totalCount",
  "storageKey": null
};
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "AllocationRulesQuery",
    "selections": [
      {
        "alias": null,
        "args": (v3/*:: as any*/),
        "concreteType": "AllocationRuleConnection",
        "kind": "LinkedField",
        "name": "allocationRules",
        "plural": false,
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "AllocationRuleEdge",
            "kind": "LinkedField",
            "name": "edges",
            "plural": true,
            "selections": [
              (v4/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": "AllocationRule",
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
                "selections": [
                  (v5/*:: as any*/),
                  (v6/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": null,
                    "kind": "LinkedField",
                    "name": "target",
                    "plural": false,
                    "selections": [
                      (v7/*:: as any*/),
                      (v8/*:: as any*/)
                    ],
                    "storageKey": null
                  },
                  (v9/*:: as any*/),
                  (v10/*:: as any*/)
                ],
                "storageKey": null
              }
            ],
            "storageKey": null
          },
          (v11/*:: as any*/),
          (v12/*:: as any*/)
        ],
        "storageKey": null
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v0/*:: as any*/),
      (v1/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "AllocationRulesQuery",
    "selections": [
      {
        "alias": null,
        "args": (v3/*:: as any*/),
        "concreteType": "AllocationRuleConnection",
        "kind": "LinkedField",
        "name": "allocationRules",
        "plural": false,
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "AllocationRuleEdge",
            "kind": "LinkedField",
            "name": "edges",
            "plural": true,
            "selections": [
              (v4/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": "AllocationRule",
                "kind": "LinkedField",
                "name": "node",
                "plural": false,
                "selections": [
                  (v5/*:: as any*/),
                  (v6/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": null,
                    "kind": "LinkedField",
                    "name": "target",
                    "plural": false,
                    "selections": [
                      (v7/*:: as any*/),
                      (v8/*:: as any*/),
                      {
                        "kind": "InlineFragment",
                        "selections": [
                          (v5/*:: as any*/)
                        ],
                        "type": "Node",
                        "abstractKey": "__isNode"
                      }
                    ],
                    "storageKey": null
                  },
                  (v9/*:: as any*/),
                  (v10/*:: as any*/)
                ],
                "storageKey": null
              }
            ],
            "storageKey": null
          },
          (v11/*:: as any*/),
          (v12/*:: as any*/)
        ],
        "storageKey": null
      }
    ]
  },
  "params": {
    "cacheID": "cf6e7fe9876e6434abfceafefe172b32",
    "id": null,
    "metadata": {},
    "name": "AllocationRulesQuery",
    "operationKind": "query",
    "text": "query AllocationRulesQuery(\n  $householdId: ID!\n  $after: String\n  $first: Int\n) {\n  allocationRules(householdId: $householdId, after: $after, first: $first) {\n    edges {\n      cursor\n      node {\n        id\n        householdId\n        target {\n          __typename\n          name\n          ... on Node {\n            __isNode: __typename\n            id\n          }\n        }\n        revision\n        allocations {\n          poolId\n          poolName\n          weight\n          amountYen\n        }\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "eceb057170cf51becb39377d1ad17fe7";

export default node;

/**
 * @generated SignedSource<<6ed25ee50dc7a1180642af7e47df34ca>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseAllocationRulesQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type PurchaseAllocationRulesQuery$data = {
  readonly allocationRules: {
    readonly edges: ReadonlyArray<{
      readonly node: {
        readonly allocations: ReadonlyArray<{
          readonly poolId: string;
          readonly weight: any;
        }>;
        readonly id: string;
        readonly target: {
          readonly __typename: string;
          readonly id?: string;
          readonly name: string;
        };
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
  };
};
export type PurchaseAllocationRulesQuery = {
  response: PurchaseAllocationRulesQuery$data;
  variables: PurchaseAllocationRulesQuery$variables;
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
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v4 = [
  {
    "alias": null,
    "args": [
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
          {
            "alias": null,
            "args": null,
            "concreteType": "AllocationRule",
            "kind": "LinkedField",
            "name": "node",
            "plural": false,
            "selections": [
              (v3/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "concreteType": null,
                "kind": "LinkedField",
                "name": "target",
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
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "name",
                    "storageKey": null
                  },
                  {
                    "kind": "InlineFragment",
                    "selections": [
                      (v3/*:: as any*/)
                    ],
                    "type": "Node",
                    "abstractKey": "__isNode"
                  }
                ],
                "storageKey": null
              },
              {
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
        "storageKey": null
      },
      {
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
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "PurchaseAllocationRulesQuery",
    "selections": (v4/*:: as any*/),
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
    "name": "PurchaseAllocationRulesQuery",
    "selections": (v4/*:: as any*/)
  },
  "params": {
    "cacheID": "e9c75be5a56ffa141d2afb8c9b33453c",
    "id": null,
    "metadata": {},
    "name": "PurchaseAllocationRulesQuery",
    "operationKind": "query",
    "text": "query PurchaseAllocationRulesQuery(\n  $householdId: ID!\n  $after: String\n  $first: Int\n) {\n  allocationRules(householdId: $householdId, after: $after, first: $first) {\n    edges {\n      node {\n        id\n        target {\n          __typename\n          name\n          ... on Node {\n            __isNode: __typename\n            id\n          }\n        }\n        allocations {\n          poolId\n          weight\n        }\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "7e7c260334658c74b033aaddfd3ba5bf";

export default node;

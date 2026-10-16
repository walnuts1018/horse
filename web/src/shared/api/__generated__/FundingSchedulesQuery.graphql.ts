/**
 * @generated SignedSource<<dd9c833a16485c8d20d3920b3562da9d>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type FundingScheduleCadence = "CUSTOM" | "MONTHLY" | "WEEKLY" | "%future added value";
export type FundingSchedulesQuery$variables = {
  active?: boolean | null | undefined;
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type FundingSchedulesQuery$data = {
  readonly fundingSchedules: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
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
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
  };
};
export type FundingSchedulesQuery = {
  response: FundingSchedulesQuery$data;
  variables: FundingSchedulesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "active"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "after"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v4 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "active",
        "variableName": "active"
      },
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
    "concreteType": "FundingScheduleConnection",
    "kind": "LinkedField",
    "name": "fundingSchedules",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "FundingScheduleEdge",
        "kind": "LinkedField",
        "name": "edges",
        "plural": true,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "cursor",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "FundingSchedule",
            "kind": "LinkedField",
            "name": "node",
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
      (v2/*:: as any*/),
      (v3/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "FundingSchedulesQuery",
    "selections": (v4/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v3/*:: as any*/),
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "FundingSchedulesQuery",
    "selections": (v4/*:: as any*/)
  },
  "params": {
    "cacheID": "6d9114f86d689e5856d16dd8fdcc77e6",
    "id": null,
    "metadata": {},
    "name": "FundingSchedulesQuery",
    "operationKind": "query",
    "text": "query FundingSchedulesQuery(\n  $householdId: ID!\n  $active: Boolean\n  $after: String\n  $first: Int\n) {\n  fundingSchedules(householdId: $householdId, active: $active, after: $after, first: $first) {\n    edges {\n      cursor\n      node {\n        id\n        householdId\n        memberId\n        poolId\n        amountYen\n        cadence\n        startsOn\n        endsOn\n        active\n        createdAt\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "f414e03e4aca1d43c57489481b01945d";

export default node;

/**
 * @generated SignedSource<<c96602056cbc7a5c5b53f885be295007>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type StatementTransferCandidatesQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type StatementTransferCandidatesQuery$data = {
  readonly ledger: {
    readonly edges: ReadonlyArray<{
      readonly node: {
        readonly __typename: string;
        readonly amountYen: any;
        readonly description: string;
        readonly fromMember?: {
          readonly displayName: string;
          readonly id: string;
        };
        readonly id: string;
        readonly occurredAt: string;
        readonly toMember?: {
          readonly displayName: string;
          readonly id: string;
        };
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
  };
};
export type StatementTransferCandidatesQuery = {
  response: StatementTransferCandidatesQuery$data;
  variables: StatementTransferCandidatesQuery$variables;
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
  (v3/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "displayName",
    "storageKey": null
  }
],
v5 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Member",
    "kind": "LinkedField",
    "name": "fromMember",
    "plural": false,
    "selections": (v4/*:: as any*/),
    "storageKey": null
  },
  {
    "alias": null,
    "args": null,
    "concreteType": "Member",
    "kind": "LinkedField",
    "name": "toMember",
    "plural": false,
    "selections": (v4/*:: as any*/),
    "storageKey": null
  }
],
v6 = [
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
    "concreteType": "LedgerConnection",
    "kind": "LinkedField",
    "name": "ledger",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "LedgerEdge",
        "kind": "LinkedField",
        "name": "edges",
        "plural": true,
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": null,
            "kind": "LinkedField",
            "name": "node",
            "plural": false,
            "selections": [
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "__typename",
                "storageKey": null
              },
              (v3/*:: as any*/),
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "occurredAt",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "description",
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
                "kind": "InlineFragment",
                "selections": (v5/*:: as any*/),
                "type": "LoanTransferLedgerEntry",
                "abstractKey": null
              },
              {
                "kind": "InlineFragment",
                "selections": (v5/*:: as any*/),
                "type": "RepaymentTransferLedgerEntry",
                "abstractKey": null
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
    "name": "StatementTransferCandidatesQuery",
    "selections": (v6/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "StatementTransferCandidatesQuery",
    "selections": (v6/*:: as any*/)
  },
  "params": {
    "cacheID": "1f5bcca560c1d12bf264bfc72a0b3013",
    "id": null,
    "metadata": {},
    "name": "StatementTransferCandidatesQuery",
    "operationKind": "query",
    "text": "query StatementTransferCandidatesQuery(\n  $householdId: ID!\n  $first: Int\n  $after: String\n) {\n  ledger(householdId: $householdId, first: $first, after: $after) {\n    edges {\n      node {\n        __typename\n        id\n        occurredAt\n        description\n        amountYen\n        ... on LoanTransferLedgerEntry {\n          fromMember {\n            id\n            displayName\n          }\n          toMember {\n            id\n            displayName\n          }\n        }\n        ... on RepaymentTransferLedgerEntry {\n          fromMember {\n            id\n            displayName\n          }\n          toMember {\n            id\n            displayName\n          }\n        }\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "ad996816e7e2b9e1997a86d3c968815b";

export default node;

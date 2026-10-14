/**
 * @generated SignedSource<<e04478dadaff59211e94cdbea221c286>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type StatementTransferCandidatesQuery$variables = {
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
  "name": "first"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v3 = [
  (v2/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "displayName",
    "storageKey": null
  }
],
v4 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Member",
    "kind": "LinkedField",
    "name": "fromMember",
    "plural": false,
    "selections": (v3/*:: as any*/),
    "storageKey": null
  },
  {
    "alias": null,
    "args": null,
    "concreteType": "Member",
    "kind": "LinkedField",
    "name": "toMember",
    "plural": false,
    "selections": (v3/*:: as any*/),
    "storageKey": null
  }
],
v5 = [
  {
    "alias": null,
    "args": [
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
              (v2/*:: as any*/),
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
                "selections": (v4/*:: as any*/),
                "type": "LoanTransferLedgerEntry",
                "abstractKey": null
              },
              {
                "kind": "InlineFragment",
                "selections": (v4/*:: as any*/),
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
      (v1/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "StatementTransferCandidatesQuery",
    "selections": (v5/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "StatementTransferCandidatesQuery",
    "selections": (v5/*:: as any*/)
  },
  "params": {
    "cacheID": "4a4209c2456053747220bbe409e1e6fd",
    "id": null,
    "metadata": {},
    "name": "StatementTransferCandidatesQuery",
    "operationKind": "query",
    "text": "query StatementTransferCandidatesQuery(\n  $householdId: ID!\n  $first: Int\n) {\n  ledger(householdId: $householdId, first: $first) {\n    edges {\n      node {\n        __typename\n        id\n        occurredAt\n        description\n        amountYen\n        ... on LoanTransferLedgerEntry {\n          fromMember {\n            id\n            displayName\n          }\n          toMember {\n            id\n            displayName\n          }\n        }\n        ... on RepaymentTransferLedgerEntry {\n          fromMember {\n            id\n            displayName\n          }\n          toMember {\n            id\n            displayName\n          }\n        }\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "b1c70a7266d5463d26f63db41d793b20";

export default node;

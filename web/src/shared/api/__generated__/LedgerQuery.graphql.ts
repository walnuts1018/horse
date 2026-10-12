/**
 * @generated SignedSource<<ea0fd1dea8210d8d57b22cfd9a768ebf>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type LedgerKind = "CORRECTION" | "FUNDING" | "PURCHASE" | "TRANSFER" | "%future added value";
export type LedgerQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
};
export type LedgerQuery$data = {
  readonly ledger: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly amountYen: any;
        readonly currency: string;
        readonly description: string;
        readonly fromMemberId: string | null | undefined;
        readonly id: string;
        readonly kind: LedgerKind;
        readonly loanId: string | null | undefined;
        readonly occurredAt: string;
        readonly poolId: string | null | undefined;
        readonly sourcePurchaseId: string | null | undefined;
        readonly toMemberId: string | null | undefined;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
    readonly totalCount: number;
  };
};
export type LedgerQuery = {
  response: LedgerQuery$data;
  variables: LedgerQuery$variables;
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
            "kind": "ScalarField",
            "name": "cursor",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "LedgerEntry",
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
                "name": "kind",
                "storageKey": null
              },
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
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "currency",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "fromMemberId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "toMemberId",
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
                "name": "sourcePurchaseId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "loanId",
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
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "totalCount",
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
    "name": "LedgerQuery",
    "selections": (v3/*:: as any*/),
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
    "name": "LedgerQuery",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "2f57e021d48fde7847e797612f788a17",
    "id": null,
    "metadata": {},
    "name": "LedgerQuery",
    "operationKind": "query",
    "text": "query LedgerQuery(\n  $householdId: ID!\n  $after: String\n  $first: Int\n) {\n  ledger(householdId: $householdId, after: $after, first: $first) {\n    edges {\n      cursor\n      node {\n        id\n        kind\n        occurredAt\n        description\n        amountYen\n        currency\n        fromMemberId\n        toMemberId\n        poolId\n        sourcePurchaseId\n        loanId\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n    totalCount\n  }\n}\n"
  }
};
})();

(node as any).hash = "2ef7e718a9b809876fa683d52513073a";

export default node;

/**
 * @generated SignedSource<<6b1519f4d155831463881a2cdd6dd02f>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseAmountTarget = "ITEM" | "PURCHASE" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type PurchaseFilter = {
  amountTarget?: PurchaseAmountTarget | null | undefined;
  categoryId?: string | null | undefined;
  from?: string | null | undefined;
  hasEvidence?: boolean | null | undefined;
  householdId: string;
  maximumAmount?: any | null | undefined;
  minimumAmount?: any | null | undefined;
  origin?: PurchaseOrigin | null | undefined;
  paymentAccountId?: string | null | undefined;
  poolId?: string | null | undefined;
  search?: string | null | undefined;
  status?: PurchaseStatus | null | undefined;
  tagId?: string | null | undefined;
  to?: string | null | undefined;
  uncategorized?: boolean | null | undefined;
};
export type RecurringExpenseMatchCandidatesQuery$variables = {
  filter: PurchaseFilter;
  first?: number | null | undefined;
};
export type RecurringExpenseMatchCandidatesQuery$data = {
  readonly purchases: {
    readonly edges: ReadonlyArray<{
      readonly node: {
        readonly id: string;
        readonly merchant: string;
        readonly occurredAt: string;
        readonly status: PurchaseStatus;
        readonly totalYen: any;
      };
    }>;
  };
};
export type RecurringExpenseMatchCandidatesQuery = {
  response: RecurringExpenseMatchCandidatesQuery$data;
  variables: RecurringExpenseMatchCandidatesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "filter"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "first"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "filter",
        "variableName": "filter"
      },
      {
        "kind": "Variable",
        "name": "first",
        "variableName": "first"
      }
    ],
    "concreteType": "PurchaseConnection",
    "kind": "LinkedField",
    "name": "purchases",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "PurchaseEdge",
        "kind": "LinkedField",
        "name": "edges",
        "plural": true,
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "Purchase",
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
                "name": "occurredAt",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "merchant",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "totalYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "status",
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
  }
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "RecurringExpenseMatchCandidatesQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "RecurringExpenseMatchCandidatesQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "7aa831e4a0fcae2059dc40ce65c042f5",
    "id": null,
    "metadata": {},
    "name": "RecurringExpenseMatchCandidatesQuery",
    "operationKind": "query",
    "text": "query RecurringExpenseMatchCandidatesQuery(\n  $filter: PurchaseFilter!\n  $first: Int\n) {\n  purchases(filter: $filter, first: $first) {\n    edges {\n      node {\n        id\n        occurredAt\n        merchant\n        totalYen\n        status\n      }\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "dc5ad0b4eef65f875b1ee61ce031cf70";

export default node;

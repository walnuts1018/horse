/**
 * @generated SignedSource<<caf708340de4e16135ad81039c41be0c>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type PurchaseCorrectionsPaginationQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  purchaseId: string;
};
export type PurchaseCorrectionsPaginationQuery$data = {
  readonly purchaseCorrections: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly __typename: string;
        readonly amountDeltaYen: any;
        readonly id: string;
        readonly itemId?: string;
        readonly occurredAt: string;
        readonly paymentAllocations: ReadonlyArray<{
          readonly account: {
            readonly id: string;
            readonly name: string;
          };
          readonly amountYen: any;
          readonly paymentAccountId: string;
        }>;
        readonly reason: string;
        readonly settlementMonth: string;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
  };
};
export type PurchaseCorrectionsPaginationQuery = {
  response: PurchaseCorrectionsPaginationQuery$data;
  variables: PurchaseCorrectionsPaginationQuery$variables;
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
  "name": "purchaseId"
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
        "name": "purchaseId",
        "variableName": "purchaseId"
      }
    ],
    "concreteType": "PurchaseCorrectionConnection",
    "kind": "LinkedField",
    "name": "purchaseCorrections",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "PurchaseCorrectionEdge",
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
                "kind": "InlineFragment",
                "selections": [
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "itemId",
                    "storageKey": null
                  }
                ],
                "type": "PurchaseItemCorrection",
                "abstractKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "amountDeltaYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "reason",
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
                "name": "settlementMonth",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "concreteType": "PurchasePayment",
                "kind": "LinkedField",
                "name": "paymentAllocations",
                "plural": true,
                "selections": [
                  {
                    "alias": null,
                    "args": null,
                    "kind": "ScalarField",
                    "name": "paymentAccountId",
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
                    "concreteType": "Account",
                    "kind": "LinkedField",
                    "name": "account",
                    "plural": false,
                    "selections": [
                      (v3/*:: as any*/),
                      {
                        "alias": null,
                        "args": null,
                        "kind": "ScalarField",
                        "name": "name",
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
    "name": "PurchaseCorrectionsPaginationQuery",
    "selections": (v4/*:: as any*/),
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
    "name": "PurchaseCorrectionsPaginationQuery",
    "selections": (v4/*:: as any*/)
  },
  "params": {
    "cacheID": "74b63346aab64619bb7bbeaf48f24191",
    "id": null,
    "metadata": {},
    "name": "PurchaseCorrectionsPaginationQuery",
    "operationKind": "query",
    "text": "query PurchaseCorrectionsPaginationQuery(\n  $purchaseId: ID!\n  $first: Int\n  $after: String\n) {\n  purchaseCorrections(purchaseId: $purchaseId, first: $first, after: $after) {\n    edges {\n      cursor\n      node {\n        __typename\n        id\n        ... on PurchaseItemCorrection {\n          itemId\n        }\n        amountDeltaYen\n        reason\n        occurredAt\n        settlementMonth\n        paymentAllocations {\n          paymentAccountId\n          amountYen\n          account {\n            id\n            name\n          }\n        }\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "bf1daef31fff0a60950505c6b6daddc8";

export default node;

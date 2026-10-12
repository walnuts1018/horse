/**
 * @generated SignedSource<<9eec836df4b9dd71c80042079bdff074>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
export type AccountKind = "BANK" | "CARD" | "CASH" | "OTHER" | "%future added value";
export type PurchaseOrigin = "AI" | "MANUAL" | "UNKNOWN" | "%future added value";
export type PurchaseStatus = "CLOSED" | "DRAFT" | "POSTED" | "%future added value";
export type SourceKind = "BANK" | "CARD" | "EMAIL" | "ORDER" | "RECEIPT" | "%future added value";
import type { FragmentRefs } from "relay-runtime";
export type PurchaseListFragment$data = {
  readonly purchases: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly corrections: ReadonlyArray<{
          readonly amountDeltaYen: any;
          readonly id: string;
          readonly itemId: string | null | undefined;
          readonly occurredAt: string;
          readonly paymentAllocations: ReadonlyArray<{
            readonly account: {
              readonly id: string;
              readonly name: string;
            } | null | undefined;
            readonly amountYen: any;
            readonly paymentAccountId: string;
          }>;
          readonly reason: string;
          readonly settlementMonth: string;
        }>;
        readonly createdAt: string;
        readonly hasEvidence: boolean;
        readonly householdId: string;
        readonly id: string;
        readonly items: ReadonlyArray<{
          readonly allocations: ReadonlyArray<{
            readonly amountYen: any | null | undefined;
            readonly poolId: string;
            readonly poolName: string | null | undefined;
            readonly weight: any;
          }>;
          readonly amountYen: any;
          readonly categoryId: string | null | undefined;
          readonly currency: string;
          readonly effectiveUnitPrice: {
            readonly denominator: string;
            readonly numeratorYen: string;
          } | null | undefined;
          readonly id: string;
          readonly name: string;
          readonly printedUnitPrice: {
            readonly amountYen: any;
            readonly basis: {
              readonly denominator: string;
              readonly numerator: string;
              readonly unit: string;
            };
          } | null | undefined;
          readonly productVariantId: string | null | undefined;
          readonly quantity: {
            readonly denominator: string;
            readonly numerator: string;
            readonly unit: string;
          } | null | undefined;
          readonly revision: number;
          readonly tags: ReadonlyArray<{
            readonly archived: boolean;
            readonly householdId: string;
            readonly id: string;
            readonly name: string;
            readonly revision: number;
          }>;
        }>;
        readonly merchant: string;
        readonly occurredAt: string;
        readonly origin: PurchaseOrigin;
        readonly paymentAccount: {
          readonly id: string;
          readonly kind: AccountKind;
          readonly name: string;
          readonly poolId: string | null | undefined;
        } | null | undefined;
        readonly paymentAccountId: string | null | undefined;
        readonly paymentAllocations: ReadonlyArray<{
          readonly account: {
            readonly id: string;
            readonly name: string;
          } | null | undefined;
          readonly amountYen: any;
          readonly paymentAccountId: string;
        }>;
        readonly revision: number;
        readonly sourceKinds: ReadonlyArray<SourceKind>;
        readonly status: PurchaseStatus;
        readonly totalYen: any;
        readonly updatedAt: string;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
      readonly hasPreviousPage: boolean;
      readonly startCursor: string | null | undefined;
    };
    readonly totalCount: number;
  };
  readonly " $fragmentType": "PurchaseListFragment";
};
export type PurchaseListFragment$key = {
  readonly " $data"?: PurchaseListFragment$data;
  readonly " $fragmentSpreads": FragmentRefs<"PurchaseListFragment">;
};

import PurchasePaginationQuery_graphql from './PurchasePaginationQuery.graphql';

const node: ReaderFragment = (function(){
var v0 = [
  "purchases"
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
  "name": "householdId",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "paymentAccountId",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v6 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "poolId",
  "storageKey": null
},
v7 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v8 = {
  "alias": null,
  "args": null,
  "concreteType": "PurchasePayment",
  "kind": "LinkedField",
  "name": "paymentAllocations",
  "plural": true,
  "selections": [
    (v4/*:: as any*/),
    (v7/*:: as any*/),
    {
      "alias": null,
      "args": null,
      "concreteType": "Account",
      "kind": "LinkedField",
      "name": "account",
      "plural": false,
      "selections": [
        (v1/*:: as any*/),
        (v5/*:: as any*/)
      ],
      "storageKey": null
    }
  ],
  "storageKey": null
},
v9 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "denominator",
  "storageKey": null
},
v10 = [
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "numerator",
    "storageKey": null
  },
  (v9/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "unit",
    "storageKey": null
  }
],
v11 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
};
return {
  "argumentDefinitions": [
    {
      "defaultValue": null,
      "kind": "LocalArgument",
      "name": "after"
    },
    {
      "defaultValue": 100,
      "kind": "LocalArgument",
      "name": "first"
    },
    {
      "defaultValue": null,
      "kind": "LocalArgument",
      "name": "from"
    },
    {
      "defaultValue": null,
      "kind": "LocalArgument",
      "name": "hasEvidence"
    },
    {
      "defaultValue": null,
      "kind": "LocalArgument",
      "name": "householdId"
    },
    {
      "defaultValue": null,
      "kind": "LocalArgument",
      "name": "origin"
    },
    {
      "defaultValue": null,
      "kind": "LocalArgument",
      "name": "to"
    }
  ],
  "kind": "Fragment",
  "metadata": {
    "connection": [
      {
        "count": "first",
        "cursor": "after",
        "direction": "forward",
        "path": (v0/*:: as any*/)
      }
    ],
    "refetch": {
      "connection": {
        "forward": {
          "count": "first",
          "cursor": "after"
        },
        "backward": null,
        "path": (v0/*:: as any*/)
      },
      "fragmentPathInResult": [],
      "operation": PurchasePaginationQuery_graphql
    }
  },
  "name": "PurchaseListFragment",
  "selections": [
    {
      "alias": "purchases",
      "args": [
        {
          "kind": "Variable",
          "name": "from",
          "variableName": "from"
        },
        {
          "kind": "Variable",
          "name": "hasEvidence",
          "variableName": "hasEvidence"
        },
        {
          "kind": "Variable",
          "name": "householdId",
          "variableName": "householdId"
        },
        {
          "kind": "Variable",
          "name": "origin",
          "variableName": "origin"
        },
        {
          "kind": "Variable",
          "name": "to",
          "variableName": "to"
        }
      ],
      "concreteType": "PurchaseConnection",
      "kind": "LinkedField",
      "name": "__PurchaseListFragment_purchases_connection",
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
              "kind": "ScalarField",
              "name": "cursor",
              "storageKey": null
            },
            {
              "alias": null,
              "args": null,
              "concreteType": "Purchase",
              "kind": "LinkedField",
              "name": "node",
              "plural": false,
              "selections": [
                (v1/*:: as any*/),
                (v2/*:: as any*/),
                (v3/*:: as any*/),
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
                  "name": "origin",
                  "storageKey": null
                },
                {
                  "alias": null,
                  "args": null,
                  "kind": "ScalarField",
                  "name": "hasEvidence",
                  "storageKey": null
                },
                {
                  "alias": null,
                  "args": null,
                  "kind": "ScalarField",
                  "name": "sourceKinds",
                  "storageKey": null
                },
                (v4/*:: as any*/),
                {
                  "alias": null,
                  "args": null,
                  "concreteType": "Account",
                  "kind": "LinkedField",
                  "name": "paymentAccount",
                  "plural": false,
                  "selections": [
                    (v1/*:: as any*/),
                    (v5/*:: as any*/),
                    {
                      "alias": null,
                      "args": null,
                      "kind": "ScalarField",
                      "name": "kind",
                      "storageKey": null
                    },
                    (v6/*:: as any*/)
                  ],
                  "storageKey": null
                },
                (v8/*:: as any*/),
                {
                  "alias": null,
                  "args": null,
                  "concreteType": "PurchaseItem",
                  "kind": "LinkedField",
                  "name": "items",
                  "plural": true,
                  "selections": [
                    (v1/*:: as any*/),
                    (v5/*:: as any*/),
                    (v7/*:: as any*/),
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
                      "concreteType": "PurchaseQuantity",
                      "kind": "LinkedField",
                      "name": "quantity",
                      "plural": false,
                      "selections": (v10/*:: as any*/),
                      "storageKey": null
                    },
                    {
                      "alias": null,
                      "args": null,
                      "concreteType": "PrintedUnitPrice",
                      "kind": "LinkedField",
                      "name": "printedUnitPrice",
                      "plural": false,
                      "selections": [
                        (v7/*:: as any*/),
                        {
                          "alias": null,
                          "args": null,
                          "concreteType": "PurchaseQuantity",
                          "kind": "LinkedField",
                          "name": "basis",
                          "plural": false,
                          "selections": (v10/*:: as any*/),
                          "storageKey": null
                        }
                      ],
                      "storageKey": null
                    },
                    {
                      "alias": null,
                      "args": null,
                      "concreteType": "EffectiveUnitPrice",
                      "kind": "LinkedField",
                      "name": "effectiveUnitPrice",
                      "plural": false,
                      "selections": [
                        {
                          "alias": null,
                          "args": null,
                          "kind": "ScalarField",
                          "name": "numeratorYen",
                          "storageKey": null
                        },
                        (v9/*:: as any*/)
                      ],
                      "storageKey": null
                    },
                    (v11/*:: as any*/),
                    {
                      "alias": null,
                      "args": null,
                      "kind": "ScalarField",
                      "name": "categoryId",
                      "storageKey": null
                    },
                    {
                      "alias": null,
                      "args": null,
                      "kind": "ScalarField",
                      "name": "productVariantId",
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
                        (v6/*:: as any*/),
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
                        (v7/*:: as any*/)
                      ],
                      "storageKey": null
                    },
                    {
                      "alias": null,
                      "args": null,
                      "concreteType": "Tag",
                      "kind": "LinkedField",
                      "name": "tags",
                      "plural": true,
                      "selections": [
                        (v1/*:: as any*/),
                        (v2/*:: as any*/),
                        (v5/*:: as any*/),
                        {
                          "alias": null,
                          "args": null,
                          "kind": "ScalarField",
                          "name": "archived",
                          "storageKey": null
                        },
                        (v11/*:: as any*/)
                      ],
                      "storageKey": null
                    }
                  ],
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
                  "concreteType": "PurchaseCorrection",
                  "kind": "LinkedField",
                  "name": "corrections",
                  "plural": true,
                  "selections": [
                    (v1/*:: as any*/),
                    {
                      "alias": null,
                      "args": null,
                      "kind": "ScalarField",
                      "name": "itemId",
                      "storageKey": null
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
                    (v3/*:: as any*/),
                    {
                      "alias": null,
                      "args": null,
                      "kind": "ScalarField",
                      "name": "settlementMonth",
                      "storageKey": null
                    },
                    (v8/*:: as any*/)
                  ],
                  "storageKey": null
                },
                (v11/*:: as any*/),
                {
                  "alias": null,
                  "args": null,
                  "kind": "ScalarField",
                  "name": "status",
                  "storageKey": null
                },
                {
                  "alias": null,
                  "args": null,
                  "kind": "ScalarField",
                  "name": "createdAt",
                  "storageKey": null
                },
                {
                  "alias": null,
                  "args": null,
                  "kind": "ScalarField",
                  "name": "updatedAt",
                  "storageKey": null
                },
                {
                  "alias": null,
                  "args": null,
                  "kind": "ScalarField",
                  "name": "__typename",
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
              "name": "startCursor",
              "storageKey": null
            },
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
            },
            {
              "alias": null,
              "args": null,
              "kind": "ScalarField",
              "name": "hasPreviousPage",
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
  ],
  "type": "Query",
  "abstractKey": null
};
})();

(node as any).hash = "903e12c244d76c765da759a3b32c5081";

export default node;

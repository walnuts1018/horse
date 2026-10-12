/**
 * @generated SignedSource<<30c0bf76698557cd7b36cf491fd5fbc1>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AllocationRuleScope = "CATEGORY" | "MERCHANT" | "PRODUCT" | "PRODUCT_VARIANT" | "%future added value";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SetAllocationRuleInput = {
  allocations: ReadonlyArray<AllocationInput>;
  categoryId?: string | null | undefined;
  householdId: string;
  merchant?: string | null | undefined;
  productConceptId?: string | null | undefined;
  productVariantId?: string | null | undefined;
};
export type AllocationInput = {
  poolId: string;
  weight: any;
};
export type SetAllocationRuleMutation$variables = {
  input: SetAllocationRuleInput;
};
export type SetAllocationRuleMutation$data = {
  readonly setAllocationRule: {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "SetAllocationRuleSuccess";
    readonly result: {
      readonly allocations: ReadonlyArray<{
        readonly poolId: string;
        readonly weight: any;
      }>;
      readonly categoryId: string | null | undefined;
      readonly id: string;
      readonly merchant: string | null | undefined;
      readonly productConceptId: string | null | undefined;
      readonly productVariantId: string | null | undefined;
      readonly revision: number;
      readonly scope: AllocationRuleScope;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type SetAllocationRuleMutation = {
  response: SetAllocationRuleMutation$data;
  variables: SetAllocationRuleMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "input"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "input",
        "variableName": "input"
      }
    ],
    "concreteType": null,
    "kind": "LinkedField",
    "name": "setAllocationRule",
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
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "AllocationRule",
            "kind": "LinkedField",
            "name": "result",
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
                "name": "scope",
                "storageKey": null
              },
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
                "name": "merchant",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "productConceptId",
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
                "kind": "ScalarField",
                "name": "revision",
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
        "type": "SetAllocationRuleSuccess",
        "abstractKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "code",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "message",
            "storageKey": null
          }
        ],
        "type": "MutationError",
        "abstractKey": null
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
    "name": "SetAllocationRuleMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetAllocationRuleMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "4987d715493585cd32a11a9142639772",
    "id": null,
    "metadata": {},
    "name": "SetAllocationRuleMutation",
    "operationKind": "mutation",
    "text": "mutation SetAllocationRuleMutation(\n  $input: SetAllocationRuleInput!\n) {\n  setAllocationRule(input: $input) {\n    __typename\n    ... on SetAllocationRuleSuccess {\n      result {\n        id\n        scope\n        categoryId\n        merchant\n        productConceptId\n        productVariantId\n        revision\n        allocations {\n          poolId\n          weight\n        }\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "c834d3520bf1ad37f9b8e4fbed081faa";

export default node;

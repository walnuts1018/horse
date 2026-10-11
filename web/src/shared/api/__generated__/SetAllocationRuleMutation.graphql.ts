/**
 * @generated SignedSource<<0deb8acc7af3546251dbf2c096c207ac>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AllocationRuleScope = "CATEGORY" | "MERCHANT" | "PRODUCT" | "PRODUCT_VARIANT" | "%future added value";
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
    "concreteType": "AllocationRule",
    "kind": "LinkedField",
    "name": "setAllocationRule",
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
    "cacheID": "969cfe7c7da31987dae4f2bdd2222ddd",
    "id": null,
    "metadata": {},
    "name": "SetAllocationRuleMutation",
    "operationKind": "mutation",
    "text": "mutation SetAllocationRuleMutation(\n  $input: SetAllocationRuleInput!\n) {\n  setAllocationRule(input: $input) {\n    id\n    scope\n    categoryId\n    merchant\n    productConceptId\n    productVariantId\n    revision\n    allocations {\n      poolId\n      weight\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "89adf44e30055f0dd842b8312e8f7886";

export default node;

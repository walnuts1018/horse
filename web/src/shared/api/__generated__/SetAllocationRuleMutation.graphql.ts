/**
 * @generated SignedSource<<f98221a226dbdad66b8af54f63a32cf5>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type SetAllocationRuleInput = {
  allocations: ReadonlyArray<SetAllocationRuleItemInput>;
  householdId: string;
  target: AllocationRuleTargetInput;
};
export type AllocationRuleTargetInput = {
  categoryId: string;
  merchant?: never;
  productConceptId?: never;
  productVariantId?: never;
} | {
  categoryId?: never;
  merchant: string;
  productConceptId?: never;
  productVariantId?: never;
} | {
  categoryId?: never;
  merchant?: never;
  productConceptId: string;
  productVariantId?: never;
} | {
  categoryId?: never;
  merchant?: never;
  productConceptId?: never;
  productVariantId: string;
};
export type SetAllocationRuleItemInput = {
  poolId: string;
  weight: any;
};
export type SetAllocationRuleMutation$variables = {
  input: SetAllocationRuleInput;
};
export type SetAllocationRuleMutation$data = {
  readonly setAllocationRule: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly allocations: ReadonlyArray<{
        readonly poolId: string;
        readonly weight: any;
      }>;
      readonly id: string;
      readonly revision: number;
      readonly target: {
        readonly __typename: string;
        readonly name: string;
      };
    };
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
    "kind": "Variable",
    "name": "input",
    "variableName": "input"
  }
],
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "__typename",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "name",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v6 = {
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
},
v7 = {
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
  "abstractKey": "__isMutationError"
};
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "SetAllocationRuleMutation",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "setAllocationRule",
        "plural": false,
        "selections": [
          (v2/*:: as any*/),
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
                  (v3/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": null,
                    "kind": "LinkedField",
                    "name": "target",
                    "plural": false,
                    "selections": [
                      (v2/*:: as any*/),
                      (v4/*:: as any*/)
                    ],
                    "storageKey": null
                  },
                  (v5/*:: as any*/),
                  (v6/*:: as any*/)
                ],
                "storageKey": null
              }
            ],
            "type": "SetAllocationRuleSuccess",
            "abstractKey": null
          },
          (v7/*:: as any*/)
        ],
        "storageKey": null
      }
    ],
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetAllocationRuleMutation",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": null,
        "kind": "LinkedField",
        "name": "setAllocationRule",
        "plural": false,
        "selections": [
          (v2/*:: as any*/),
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
                  (v3/*:: as any*/),
                  {
                    "alias": null,
                    "args": null,
                    "concreteType": null,
                    "kind": "LinkedField",
                    "name": "target",
                    "plural": false,
                    "selections": [
                      (v2/*:: as any*/),
                      (v4/*:: as any*/),
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
                  (v5/*:: as any*/),
                  (v6/*:: as any*/)
                ],
                "storageKey": null
              }
            ],
            "type": "SetAllocationRuleSuccess",
            "abstractKey": null
          },
          (v7/*:: as any*/)
        ],
        "storageKey": null
      }
    ]
  },
  "params": {
    "cacheID": "24ec6059bade31ba339d84713d29c495",
    "id": null,
    "metadata": {},
    "name": "SetAllocationRuleMutation",
    "operationKind": "mutation",
    "text": "mutation SetAllocationRuleMutation(\n  $input: SetAllocationRuleInput!\n) {\n  setAllocationRule(input: $input) {\n    __typename\n    ... on SetAllocationRuleSuccess {\n      result {\n        id\n        target {\n          __typename\n          name\n          ... on Node {\n            __isNode: __typename\n            id\n          }\n        }\n        revision\n        allocations {\n          poolId\n          weight\n        }\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "e30b369aecf139c9df91be6eea3e508a";

export default node;

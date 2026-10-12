/**
 * @generated SignedSource<<e89efc88bb53d01cc54b3a233f3777f0>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type SetProductAliasInput = {
  alias: string;
  householdId: string;
  merchant: string;
  variantId: string;
};
export type SetProductAliasMutation$variables = {
  input: SetProductAliasInput;
};
export type SetProductAliasMutation$data = {
  readonly setProductAlias: {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    readonly __typename: "SetProductAliasSuccess";
    readonly result: {
      readonly alias: string;
      readonly id: string;
      readonly merchantKey: string;
      readonly revision: number;
    };
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type SetProductAliasMutation = {
  response: SetProductAliasMutation$data;
  variables: SetProductAliasMutation$variables;
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
    "name": "setProductAlias",
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
            "concreteType": "ProductAlias",
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
                "name": "alias",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "merchantKey",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "revision",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "SetProductAliasSuccess",
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
    "name": "SetProductAliasMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetProductAliasMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "280b58f6d60dce65986d6fe7a86d6045",
    "id": null,
    "metadata": {},
    "name": "SetProductAliasMutation",
    "operationKind": "mutation",
    "text": "mutation SetProductAliasMutation(\n  $input: SetProductAliasInput!\n) {\n  setProductAlias(input: $input) {\n    __typename\n    ... on SetProductAliasSuccess {\n      result {\n        id\n        alias\n        merchantKey\n        revision\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "563d6654f330484b0b8a269b651afcc5";

export default node;

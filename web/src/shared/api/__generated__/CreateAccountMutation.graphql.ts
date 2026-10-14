/**
 * @generated SignedSource<<b9985c342048e20d8c8433fdd068ac7f>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountKind = "BANK" | "CARD" | "CASH" | "OTHER" | "%future added value";
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type CreateAccountInput = {
  cardLast4?: string | null | undefined;
  householdId: string;
  kind: AccountKind;
  name: string;
  openingBalance?: AccountOpeningBalanceInput | null | undefined;
  poolId?: string | null | undefined;
};
export type AccountOpeningBalanceInput = {
  amountYen: any;
  at: string;
};
export type CreateAccountMutation$variables = {
  input: CreateAccountInput;
};
export type CreateAccountMutation$data = {
  readonly createAccount: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly id: string;
    };
  };
};
export type CreateAccountMutation = {
  response: CreateAccountMutation$data;
  variables: CreateAccountMutation$variables;
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
    "name": "createAccount",
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
            "concreteType": "Account",
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
              }
            ],
            "storageKey": null
          }
        ],
        "type": "CreateAccountSuccess",
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
        "abstractKey": "__isMutationError"
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
    "name": "CreateAccountMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateAccountMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "a902c3f65459ee51568e0fc40e1e499c",
    "id": null,
    "metadata": {},
    "name": "CreateAccountMutation",
    "operationKind": "mutation",
    "text": "mutation CreateAccountMutation(\n  $input: CreateAccountInput!\n) {\n  createAccount(input: $input) {\n    __typename\n    ... on CreateAccountSuccess {\n      result {\n        id\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "93a33dfb00d3a0aa1aa4356df08072ae";

export default node;

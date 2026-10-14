/**
 * @generated SignedSource<<55c55bb8eee75a836120d05ad9434690>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type LinkStatementAccountIncomeInput = {
  accountIncomeId: string;
  householdId: string;
  sourceAccountId: string;
  sourceDocumentId: string;
  sourceReference: string;
};
export type LinkStatementAccountIncomeMutation$variables = {
  input: LinkStatementAccountIncomeInput;
};
export type LinkStatementAccountIncomeMutation$data = {
  readonly linkStatementAccountIncome: {
    readonly __typename: "LinkStatementAccountIncomeSuccess";
    readonly result: {
      readonly accountId: string;
      readonly amountYen: any;
      readonly description: string;
      readonly householdId: string;
      readonly id: string;
      readonly occurredAt: string;
    };
  } | {
    readonly __typename: "LinkStatementAccountIncomeUserError";
    readonly message: string;
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type LinkStatementAccountIncomeMutation = {
  response: LinkStatementAccountIncomeMutation$data;
  variables: LinkStatementAccountIncomeMutation$variables;
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
    "name": "linkStatementAccountIncome",
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
            "concreteType": "AccountIncome",
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
                "name": "householdId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "accountId",
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
                "name": "occurredAt",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "description",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "type": "LinkStatementAccountIncomeSuccess",
        "abstractKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "message",
            "storageKey": null
          }
        ],
        "type": "LinkStatementAccountIncomeUserError",
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
    "name": "LinkStatementAccountIncomeMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "LinkStatementAccountIncomeMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "7972edcb32c936d26d0803f32c6b778d",
    "id": null,
    "metadata": {},
    "name": "LinkStatementAccountIncomeMutation",
    "operationKind": "mutation",
    "text": "mutation LinkStatementAccountIncomeMutation(\n  $input: LinkStatementAccountIncomeInput!\n) {\n  linkStatementAccountIncome(input: $input) {\n    __typename\n    ... on LinkStatementAccountIncomeSuccess {\n      result {\n        id\n        householdId\n        accountId\n        amountYen\n        occurredAt\n        description\n      }\n    }\n    ... on LinkStatementAccountIncomeUserError {\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "d35a4ecd6ea7b99ebd98ba16542b387a";

export default node;

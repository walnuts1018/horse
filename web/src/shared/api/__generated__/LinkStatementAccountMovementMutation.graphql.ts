/**
 * @generated SignedSource<<a697fecb9576ba123191c568a38a6255>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type LinkStatementAccountMovementInput = {
  accountMovementId: string;
  householdId: string;
  sourceAccountId: string;
  sourceDocumentId: string;
  sourceReference: string;
};
export type LinkStatementAccountMovementMutation$variables = {
  input: LinkStatementAccountMovementInput;
};
export type LinkStatementAccountMovementMutation$data = {
  readonly linkStatementAccountMovement: {
    readonly __typename: "LinkStatementAccountMovementSuccess";
    readonly result: {
      readonly id: string;
    };
  } | {
    readonly __typename: "LinkStatementAccountMovementUserError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type LinkStatementAccountMovementMutation = {
  response: LinkStatementAccountMovementMutation$data;
  variables: LinkStatementAccountMovementMutation$variables;
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
    "name": "linkStatementAccountMovement",
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
            "concreteType": "AccountMovement",
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
        "type": "LinkStatementAccountMovementSuccess",
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
        "type": "LinkStatementAccountMovementUserError",
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
    "name": "LinkStatementAccountMovementMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "LinkStatementAccountMovementMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "f8c0d8f50434e3e4c7c0effd8048cf5a",
    "id": null,
    "metadata": {},
    "name": "LinkStatementAccountMovementMutation",
    "operationKind": "mutation",
    "text": "mutation LinkStatementAccountMovementMutation(\n  $input: LinkStatementAccountMovementInput!\n) {\n  linkStatementAccountMovement(input: $input) {\n    __typename\n    ... on LinkStatementAccountMovementSuccess {\n      result {\n        id\n      }\n    }\n    ... on LinkStatementAccountMovementUserError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "755996acee3009cb12344721ecf42c32";

export default node;

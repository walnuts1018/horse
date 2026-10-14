/**
 * @generated SignedSource<<17987ae2785fd5026e2058d99447b580>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type LinkStatementRefundCorrectionInput = {
  correctionId: string;
  householdId: string;
  sourceDocumentId: string;
  sourceReference: string;
};
export type LinkStatementRefundCorrectionMutation$variables = {
  input: LinkStatementRefundCorrectionInput;
};
export type LinkStatementRefundCorrectionMutation$data = {
  readonly linkStatementRefundCorrection: {
    readonly __typename: "LinkStatementRefundCorrectionSuccess";
    readonly result: {
      readonly id: string;
    };
  } | {
    readonly __typename: "LinkStatementRefundCorrectionUserError";
    readonly message: string;
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
  };
};
export type LinkStatementRefundCorrectionMutation = {
  response: LinkStatementRefundCorrectionMutation$data;
  variables: LinkStatementRefundCorrectionMutation$variables;
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
    "name": "linkStatementRefundCorrection",
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
            "concreteType": "Purchase",
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
        "type": "LinkStatementRefundCorrectionSuccess",
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
        "type": "LinkStatementRefundCorrectionUserError",
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
    "name": "LinkStatementRefundCorrectionMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "LinkStatementRefundCorrectionMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "d607230cbecbd6f243406a645816af92",
    "id": null,
    "metadata": {},
    "name": "LinkStatementRefundCorrectionMutation",
    "operationKind": "mutation",
    "text": "mutation LinkStatementRefundCorrectionMutation(\n  $input: LinkStatementRefundCorrectionInput!\n) {\n  linkStatementRefundCorrection(input: $input) {\n    __typename\n    ... on LinkStatementRefundCorrectionSuccess {\n      result {\n        id\n      }\n    }\n    ... on LinkStatementRefundCorrectionUserError {\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "c61ea81394d5597f88e0e91a3a6f7c2a";

export default node;

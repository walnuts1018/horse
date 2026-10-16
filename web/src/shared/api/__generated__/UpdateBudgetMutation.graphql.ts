/**
 * @generated SignedSource<<64adb4d591ad708c8b38fa80f2de0958>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "MAIL_FORWARDING_DISABLED" | "NOT_FOUND" | "%future added value";
export type UpdateBudgetInput = {
  amountYen: any;
  baseRevision: number;
  budgetId: string;
  categoryId?: string | null | undefined;
  effectiveFrom: string;
  endsOn?: string | null | undefined;
  householdId: string;
  name: string;
  startsOn: string;
};
export type UpdateBudgetMutation$variables = {
  input: UpdateBudgetInput;
};
export type UpdateBudgetMutation$data = {
  readonly updateBudget: {
    readonly __typename: string;
    readonly code?: MutationErrorCode;
    readonly message?: string;
    readonly result?: {
      readonly actualYen: any;
      readonly amountYen: any;
      readonly archived: boolean;
      readonly categoryId: string | null | undefined;
      readonly endsOn: string | null | undefined;
      readonly id: string;
      readonly name: string;
      readonly remainingYen: any;
      readonly revision: number;
      readonly startsOn: string;
    };
  };
};
export type UpdateBudgetMutation = {
  response: UpdateBudgetMutation$data;
  variables: UpdateBudgetMutation$variables;
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
    "name": "updateBudget",
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
            "concreteType": "Budget",
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
                "name": "categoryId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "name",
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
                "name": "actualYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "remainingYen",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "startsOn",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "endsOn",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "archived",
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
        "type": "UpdateBudgetSuccess",
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
    "name": "UpdateBudgetMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "UpdateBudgetMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "ac525eebe0cb2519409dea6eaaf0e94e",
    "id": null,
    "metadata": {},
    "name": "UpdateBudgetMutation",
    "operationKind": "mutation",
    "text": "mutation UpdateBudgetMutation(\n  $input: UpdateBudgetInput!\n) {\n  updateBudget(input: $input) {\n    __typename\n    ... on UpdateBudgetSuccess {\n      result {\n        id\n        categoryId\n        name\n        amountYen\n        actualYen\n        remainingYen\n        startsOn\n        endsOn\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      __isMutationError: __typename\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "9c67435c15b7be4854abbbfefbac6853";

export default node;

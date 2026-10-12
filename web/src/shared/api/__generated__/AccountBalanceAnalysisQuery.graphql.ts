/**
 * @generated SignedSource<<5703f7627136bcc49123d56fd77ab087>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountBalanceAnalysisQuery$variables = {
  accountId: string;
  from: string;
  householdId: string;
  to: string;
};
export type AccountBalanceAnalysisQuery$data = {
  readonly accountBalanceAnalysis: {
    readonly accountId: string;
    readonly changes: ReadonlyArray<{
      readonly balanceYen: any;
      readonly changeYen: any;
      readonly description: string;
      readonly occurredAt: string;
      readonly sourceId: string;
      readonly sourceKind: string;
    }>;
    readonly closingBalanceYen: any;
    readonly currency: string;
    readonly from: string;
    readonly openingBalanceYen: any;
    readonly to: string;
  };
};
export type AccountBalanceAnalysisQuery = {
  response: AccountBalanceAnalysisQuery$data;
  variables: AccountBalanceAnalysisQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "accountId"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "from"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "to"
},
v4 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "accountId",
        "variableName": "accountId"
      },
      {
        "kind": "Variable",
        "name": "from",
        "variableName": "from"
      },
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      },
      {
        "kind": "Variable",
        "name": "to",
        "variableName": "to"
      }
    ],
    "concreteType": "AccountBalanceAnalysis",
    "kind": "LinkedField",
    "name": "accountBalanceAnalysis",
    "plural": false,
    "selections": [
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
        "name": "currency",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "from",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "to",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "openingBalanceYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "closingBalanceYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "AccountBalanceChange",
        "kind": "LinkedField",
        "name": "changes",
        "plural": true,
        "selections": [
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
            "name": "sourceKind",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "sourceId",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "description",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "changeYen",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "balanceYen",
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
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v3/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "AccountBalanceAnalysisQuery",
    "selections": (v4/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v3/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "AccountBalanceAnalysisQuery",
    "selections": (v4/*:: as any*/)
  },
  "params": {
    "cacheID": "361e68af9e3d5788422a782260d0c654",
    "id": null,
    "metadata": {},
    "name": "AccountBalanceAnalysisQuery",
    "operationKind": "query",
    "text": "query AccountBalanceAnalysisQuery(\n  $householdId: ID!\n  $accountId: ID!\n  $from: String!\n  $to: String!\n) {\n  accountBalanceAnalysis(householdId: $householdId, accountId: $accountId, from: $from, to: $to) {\n    accountId\n    currency\n    from\n    to\n    openingBalanceYen\n    closingBalanceYen\n    changes {\n      occurredAt\n      sourceKind\n      sourceId\n      description\n      changeYen\n      balanceYen\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "4a85579ec1da5bc4a39d1827fad9e6de";

export default node;

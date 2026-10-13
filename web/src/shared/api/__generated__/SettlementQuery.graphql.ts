/**
 * @generated SignedSource<<41b5857607172211b32664d5ba754569>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SettlementStatus = "CLOSED" | "OPEN" | "%future added value";
export type SettlementQuery$variables = {
  householdId: string;
  period: string;
};
export type SettlementQuery$data = {
  readonly settlement: {
    readonly lines: ReadonlyArray<{
      readonly advanceCreditYen: any;
      readonly amountDueYen: any;
      readonly carryYen: any;
      readonly memberId: string;
      readonly memberName: string;
      readonly monthlyAdjustmentYen: any;
      readonly paidYen: any;
      readonly priorBalanceYen: any;
      readonly scheduledYen: any;
    }>;
    readonly period: string;
    readonly revision: number;
    readonly status: SettlementStatus;
  };
};
export type SettlementQuery = {
  response: SettlementQuery$data;
  variables: SettlementQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
  },
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "period"
  }
],
v1 = [
  {
    "kind": "Variable",
    "name": "householdId",
    "variableName": "householdId"
  },
  {
    "kind": "Variable",
    "name": "period",
    "variableName": "period"
  }
],
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "period",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "status",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "revision",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "concreteType": "SettlementLine",
  "kind": "LinkedField",
  "name": "lines",
  "plural": true,
  "selections": [
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "memberId",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "memberName",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "scheduledYen",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "paidYen",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "carryYen",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "priorBalanceYen",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "monthlyAdjustmentYen",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "advanceCreditYen",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "amountDueYen",
      "storageKey": null
    }
  ],
  "storageKey": null
};
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "SettlementQuery",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": "Settlement",
        "kind": "LinkedField",
        "name": "settlement",
        "plural": false,
        "selections": [
          (v2/*:: as any*/),
          (v3/*:: as any*/),
          (v4/*:: as any*/),
          (v5/*:: as any*/)
        ],
        "storageKey": null
      }
    ],
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SettlementQuery",
    "selections": [
      {
        "alias": null,
        "args": (v1/*:: as any*/),
        "concreteType": "Settlement",
        "kind": "LinkedField",
        "name": "settlement",
        "plural": false,
        "selections": [
          (v2/*:: as any*/),
          (v3/*:: as any*/),
          (v4/*:: as any*/),
          (v5/*:: as any*/),
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
    ]
  },
  "params": {
    "cacheID": "4214cbb4d20227981b8c3de44987b44a",
    "id": null,
    "metadata": {},
    "name": "SettlementQuery",
    "operationKind": "query",
    "text": "query SettlementQuery(\n  $householdId: ID!\n  $period: String!\n) {\n  settlement(householdId: $householdId, period: $period) {\n    period\n    status\n    revision\n    lines {\n      memberId\n      memberName\n      scheduledYen\n      paidYen\n      carryYen\n      priorBalanceYen\n      monthlyAdjustmentYen\n      advanceCreditYen\n      amountDueYen\n    }\n    id\n  }\n}\n"
  }
};
})();

(node as any).hash = "186ff6623f2c5f68749aa105e41a1134";

export default node;

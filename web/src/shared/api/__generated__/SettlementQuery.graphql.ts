/**
 * @generated SignedSource<<4a487353f6c2ff4035461fcbbf85380f>>
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
    "alias": null,
    "args": [
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
    "concreteType": "Settlement",
    "kind": "LinkedField",
    "name": "settlement",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "period",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "status",
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
    "name": "SettlementQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SettlementQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "04f9f0b9775751ffa41ae50cff3b0d88",
    "id": null,
    "metadata": {},
    "name": "SettlementQuery",
    "operationKind": "query",
    "text": "query SettlementQuery(\n  $householdId: ID!\n  $period: String!\n) {\n  settlement(householdId: $householdId, period: $period) {\n    period\n    status\n    revision\n    lines {\n      memberId\n      memberName\n      scheduledYen\n      paidYen\n      carryYen\n      priorBalanceYen\n      monthlyAdjustmentYen\n      advanceCreditYen\n      amountDueYen\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "186ff6623f2c5f68749aa105e41a1134";

export default node;

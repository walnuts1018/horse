/**
 * @generated SignedSource<<18a5cdd5261093fee298af57bd6b8623>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SettlementQuery$variables = {
  householdId: string;
  period: string;
};
export type SettlementQuery$data = {
  readonly settlement: {
    readonly __typename: string;
    readonly createdAt?: string;
    readonly id: string;
    readonly lines: ReadonlyArray<{
      readonly advanceCreditYen: any;
      readonly amountDueYen: any;
      readonly carryYen: any;
      readonly evidence: ReadonlyArray<{
        readonly amountYen: any;
        readonly kind: string;
        readonly label: string;
        readonly occurredAt: string | null | undefined;
        readonly purchaseId: string | null | undefined;
        readonly sourceId: string;
      }>;
      readonly memberId: string;
      readonly memberName: string;
      readonly monthlyAdjustmentYen: any;
      readonly paidYen: any;
      readonly priorBalanceYen: any;
      readonly scheduledYen: any;
    }>;
    readonly period: string;
    readonly revision: number;
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
    "concreteType": null,
    "kind": "LinkedField",
    "name": "settlement",
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
        "name": "period",
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
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "SettlementEvidence",
            "kind": "LinkedField",
            "name": "evidence",
            "plural": true,
            "selections": [
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "kind",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "label",
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
                "name": "sourceId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "purchaseId",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "occurredAt",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "createdAt",
            "storageKey": null
          }
        ],
        "type": "ClosedSettlement",
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
    "cacheID": "d24bc39b53011d199c2c00f26b866874",
    "id": null,
    "metadata": {},
    "name": "SettlementQuery",
    "operationKind": "query",
    "text": "query SettlementQuery(\n  $householdId: ID!\n  $period: YearMonth!\n) {\n  settlement(householdId: $householdId, period: $period) {\n    __typename\n    id\n    period\n    revision\n    lines {\n      memberId\n      memberName\n      scheduledYen\n      paidYen\n      carryYen\n      priorBalanceYen\n      monthlyAdjustmentYen\n      advanceCreditYen\n      amountDueYen\n      evidence {\n        kind\n        label\n        amountYen\n        sourceId\n        purchaseId\n        occurredAt\n      }\n    }\n    ... on ClosedSettlement {\n      createdAt\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "8eec0cde3255b0623fb074ce7f177a60";

export default node;

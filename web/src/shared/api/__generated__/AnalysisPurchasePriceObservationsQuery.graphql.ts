/**
 * @generated SignedSource<<715a8f320b45fb5a90249fe782b07e65>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AnalysisPurchasePriceObservationsQuery$variables = {
  from: string;
  householdId: string;
  itemIds: ReadonlyArray<string>;
  to: string;
};
export type AnalysisPurchasePriceObservationsQuery$data = {
  readonly purchasePriceObservations: ReadonlyArray<{
    readonly amountYen: any;
    readonly effectiveUnitPriceDenominator: string | null | undefined;
    readonly effectiveUnitPriceNumeratorYen: string | null | undefined;
    readonly itemId: string;
    readonly occurredAt: string;
    readonly printedUnitPriceQuantityDenominator: string | null | undefined;
    readonly printedUnitPriceQuantityNumerator: string | null | undefined;
    readonly printedUnitPriceUnit: string | null | undefined;
    readonly printedUnitPriceYen: any | null | undefined;
    readonly remainingRefundableAmountYen: any;
  }>;
};
export type AnalysisPurchasePriceObservationsQuery = {
  response: AnalysisPurchasePriceObservationsQuery$data;
  variables: AnalysisPurchasePriceObservationsQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "from"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "itemIds"
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
        "name": "itemIds",
        "variableName": "itemIds"
      },
      {
        "kind": "Variable",
        "name": "to",
        "variableName": "to"
      }
    ],
    "concreteType": "PurchasePriceObservation",
    "kind": "LinkedField",
    "name": "purchasePriceObservations",
    "plural": true,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "itemId",
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
        "name": "amountYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "remainingRefundableAmountYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "effectiveUnitPriceNumeratorYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "effectiveUnitPriceDenominator",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "printedUnitPriceYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "printedUnitPriceQuantityNumerator",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "printedUnitPriceQuantityDenominator",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "printedUnitPriceUnit",
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
    "name": "AnalysisPurchasePriceObservationsQuery",
    "selections": (v4/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/),
      (v3/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "AnalysisPurchasePriceObservationsQuery",
    "selections": (v4/*:: as any*/)
  },
  "params": {
    "cacheID": "ebe94b5fc639ff8a9a513a34757a5768",
    "id": null,
    "metadata": {},
    "name": "AnalysisPurchasePriceObservationsQuery",
    "operationKind": "query",
    "text": "query AnalysisPurchasePriceObservationsQuery(\n  $householdId: ID!\n  $from: YearMonth!\n  $to: YearMonth!\n  $itemIds: [String!]!\n) {\n  purchasePriceObservations(householdId: $householdId, from: $from, to: $to, itemIds: $itemIds) {\n    itemId\n    occurredAt\n    amountYen\n    remainingRefundableAmountYen\n    effectiveUnitPriceNumeratorYen\n    effectiveUnitPriceDenominator\n    printedUnitPriceYen\n    printedUnitPriceQuantityNumerator\n    printedUnitPriceQuantityDenominator\n    printedUnitPriceUnit\n  }\n}\n"
  }
};
})();

(node as any).hash = "21ac764952d23a4c9098e7d414da812c";

export default node;

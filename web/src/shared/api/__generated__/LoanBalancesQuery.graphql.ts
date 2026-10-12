/**
 * @generated SignedSource<<5e2efd656d443abcb994595e7824b355>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type LoanBalancesQuery$variables = {
  householdId: string;
};
export type LoanBalancesQuery$data = {
  readonly loanBalances: ReadonlyArray<{
    readonly balanceYen: any;
    readonly borrowerMemberId: string;
    readonly lenderMemberId: string;
    readonly loanId: string;
    readonly principalYen: any;
    readonly repaidYen: any;
  }>;
};
export type LoanBalancesQuery = {
  response: LoanBalancesQuery$data;
  variables: LoanBalancesQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "householdId"
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
      }
    ],
    "concreteType": "LoanBalance",
    "kind": "LinkedField",
    "name": "loanBalances",
    "plural": true,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "loanId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "lenderMemberId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "borrowerMemberId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "principalYen",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "repaidYen",
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
];
return {
  "fragment": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "LoanBalancesQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "LoanBalancesQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "28ac538ee3edd5b4d6f0ad7aa5a30ce0",
    "id": null,
    "metadata": {},
    "name": "LoanBalancesQuery",
    "operationKind": "query",
    "text": "query LoanBalancesQuery(\n  $householdId: ID!\n) {\n  loanBalances(householdId: $householdId) {\n    loanId\n    lenderMemberId\n    borrowerMemberId\n    principalYen\n    repaidYen\n    balanceYen\n  }\n}\n"
  }
};
})();

(node as any).hash = "7c0432cf729371a54d8aef37ab13df33";

export default node;

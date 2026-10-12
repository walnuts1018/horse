/**
 * @generated SignedSource<<2e99cf46dad7482f604fdb5fd2ebf2cb>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type ArchiveRecurringExpenseMutation$variables = {
  archived: boolean;
  baseRevision: number;
  expenseId: string;
};
export type ArchiveRecurringExpenseMutation$data = {
  readonly archiveRecurringExpense: {
    readonly archived: boolean;
    readonly id: string;
    readonly revision: number;
  };
};
export type ArchiveRecurringExpenseMutation = {
  response: ArchiveRecurringExpenseMutation$data;
  variables: ArchiveRecurringExpenseMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "archived"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "baseRevision"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "expenseId"
},
v3 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "archived",
        "variableName": "archived"
      },
      {
        "kind": "Variable",
        "name": "baseRevision",
        "variableName": "baseRevision"
      },
      {
        "kind": "Variable",
        "name": "expenseId",
        "variableName": "expenseId"
      }
    ],
    "concreteType": "RecurringExpense",
    "kind": "LinkedField",
    "name": "archiveRecurringExpense",
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
];
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "ArchiveRecurringExpenseMutation",
    "selections": (v3/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "ArchiveRecurringExpenseMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "4c0e7ac2798c5e86993ff2a2a0b63fb8",
    "id": null,
    "metadata": {},
    "name": "ArchiveRecurringExpenseMutation",
    "operationKind": "mutation",
    "text": "mutation ArchiveRecurringExpenseMutation(\n  $expenseId: ID!\n  $baseRevision: Int!\n  $archived: Boolean!\n) {\n  archiveRecurringExpense(expenseId: $expenseId, baseRevision: $baseRevision, archived: $archived) {\n    id\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "a4f25a66986b4077b992f92a24b97db9";

export default node;

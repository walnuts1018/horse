/**
 * @generated SignedSource<<dca04da9c37fc182150db99cd8099b49>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type ArchiveBudgetMutation$variables = {
  archived: boolean;
  baseRevision: number;
  budgetId: string;
};
export type ArchiveBudgetMutation$data = {
  readonly archiveBudget: {
    readonly archived: boolean;
    readonly id: string;
    readonly revision: number;
  };
};
export type ArchiveBudgetMutation = {
  response: ArchiveBudgetMutation$data;
  variables: ArchiveBudgetMutation$variables;
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
  "name": "budgetId"
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
        "name": "budgetId",
        "variableName": "budgetId"
      }
    ],
    "concreteType": "Budget",
    "kind": "LinkedField",
    "name": "archiveBudget",
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
    "name": "ArchiveBudgetMutation",
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
    "name": "ArchiveBudgetMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "f1f37952896566b84d32ac60b9d7867b",
    "id": null,
    "metadata": {},
    "name": "ArchiveBudgetMutation",
    "operationKind": "mutation",
    "text": "mutation ArchiveBudgetMutation(\n  $budgetId: ID!\n  $baseRevision: Int!\n  $archived: Boolean!\n) {\n  archiveBudget(budgetId: $budgetId, baseRevision: $baseRevision, archived: $archived) {\n    id\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "0d454db68ac8b79dbcf90dd19e294d98";

export default node;

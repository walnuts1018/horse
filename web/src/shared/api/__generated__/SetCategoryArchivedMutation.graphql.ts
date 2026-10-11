/**
 * @generated SignedSource<<ede91cdb15c4b8eaa8c4702341bfc8db>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SetCategoryArchivedMutation$variables = {
  archived: boolean;
  categoryId: string;
};
export type SetCategoryArchivedMutation$data = {
  readonly setCategoryArchived: {
    readonly archived: boolean;
    readonly id: string;
  };
};
export type SetCategoryArchivedMutation = {
  response: SetCategoryArchivedMutation$data;
  variables: SetCategoryArchivedMutation$variables;
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
  "name": "categoryId"
},
v2 = [
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
        "name": "categoryId",
        "variableName": "categoryId"
      }
    ],
    "concreteType": "Category",
    "kind": "LinkedField",
    "name": "setCategoryArchived",
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
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "SetCategoryArchivedMutation",
    "selections": (v2/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "SetCategoryArchivedMutation",
    "selections": (v2/*:: as any*/)
  },
  "params": {
    "cacheID": "bba357bee8d2b72f7108dad6a782329c",
    "id": null,
    "metadata": {},
    "name": "SetCategoryArchivedMutation",
    "operationKind": "mutation",
    "text": "mutation SetCategoryArchivedMutation(\n  $categoryId: ID!\n  $archived: Boolean!\n) {\n  setCategoryArchived(categoryId: $categoryId, archived: $archived) {\n    id\n    archived\n  }\n}\n"
  }
};
})();

(node as any).hash = "ba62a4d896ef41c2e7b2f90dd3347aa3";

export default node;

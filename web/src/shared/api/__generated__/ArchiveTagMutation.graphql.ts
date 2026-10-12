/**
 * @generated SignedSource<<ebb73994f305b4bfd45c6bd02138c749>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type ArchiveTagMutation$variables = {
  archived: boolean;
  baseRevision: number;
  tagId: string;
};
export type ArchiveTagMutation$data = {
  readonly archiveTag: {
    readonly archived: boolean;
    readonly id: string;
    readonly revision: number;
  };
};
export type ArchiveTagMutation = {
  response: ArchiveTagMutation$data;
  variables: ArchiveTagMutation$variables;
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
  "name": "tagId"
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
        "name": "tagId",
        "variableName": "tagId"
      }
    ],
    "concreteType": "Tag",
    "kind": "LinkedField",
    "name": "archiveTag",
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
    "name": "ArchiveTagMutation",
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
    "name": "ArchiveTagMutation",
    "selections": (v3/*:: as any*/)
  },
  "params": {
    "cacheID": "b37b251734a32e537dd1a70bb37682bb",
    "id": null,
    "metadata": {},
    "name": "ArchiveTagMutation",
    "operationKind": "mutation",
    "text": "mutation ArchiveTagMutation(\n  $tagId: ID!\n  $baseRevision: Int!\n  $archived: Boolean!\n) {\n  archiveTag(tagId: $tagId, baseRevision: $baseRevision, archived: $archived) {\n    id\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "73fe0c2fea1d8d1abee3ad1be35271b8";

export default node;

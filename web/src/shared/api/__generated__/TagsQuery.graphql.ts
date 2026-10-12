/**
 * @generated SignedSource<<4c477ca07095b3e813b46d138c2c0c5d>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type TagsQuery$variables = {
  householdId: string;
  includeArchived?: boolean | null | undefined;
};
export type TagsQuery$data = {
  readonly tags: ReadonlyArray<{
    readonly archived: boolean;
    readonly householdId: string;
    readonly id: string;
    readonly name: string;
    readonly revision: number;
  }>;
};
export type TagsQuery = {
  response: TagsQuery$data;
  variables: TagsQuery$variables;
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
    "name": "includeArchived"
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
        "name": "includeArchived",
        "variableName": "includeArchived"
      }
    ],
    "concreteType": "Tag",
    "kind": "LinkedField",
    "name": "tags",
    "plural": true,
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
        "name": "householdId",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "name",
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
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Fragment",
    "metadata": null,
    "name": "TagsQuery",
    "selections": (v1/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "TagsQuery",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "7398c948d6cfde330b931dd4a4e8e518",
    "id": null,
    "metadata": {},
    "name": "TagsQuery",
    "operationKind": "query",
    "text": "query TagsQuery(\n  $householdId: ID!\n  $includeArchived: Boolean\n) {\n  tags(householdId: $householdId, includeArchived: $includeArchived) {\n    id\n    householdId\n    name\n    archived\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "56d284a85628ee27b7fa019d8aa31615";

export default node;

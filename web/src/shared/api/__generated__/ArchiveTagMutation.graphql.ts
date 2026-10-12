/**
 * @generated SignedSource<<ba23574429ccabb2e7013cb2a7a4407a>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type MutationErrorCode = "BAD_INPUT" | "CONFLICT" | "NOT_FOUND" | "%future added value";
export type ArchiveTagMutation$variables = {
  archived: boolean;
  baseRevision: number;
  tagId: string;
};
export type ArchiveTagMutation$data = {
  readonly archiveTag: {
    readonly __typename: "ArchiveTagSuccess";
    readonly result: {
      readonly archived: boolean;
      readonly id: string;
      readonly revision: number;
    };
  } | {
    readonly __typename: "MutationError";
    readonly code: MutationErrorCode;
    readonly message: string;
  } | {
    // This will never be '%other', but we need some
    // value in case none of the concrete values match.
    readonly __typename: "%other";
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
    "concreteType": null,
    "kind": "LinkedField",
    "name": "archiveTag",
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
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "concreteType": "Tag",
            "kind": "LinkedField",
            "name": "result",
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
        ],
        "type": "ArchiveTagSuccess",
        "abstractKey": null
      },
      {
        "kind": "InlineFragment",
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "code",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "message",
            "storageKey": null
          }
        ],
        "type": "MutationError",
        "abstractKey": null
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
    "cacheID": "69f58eba9671a508d912c559b92c6eb3",
    "id": null,
    "metadata": {},
    "name": "ArchiveTagMutation",
    "operationKind": "mutation",
    "text": "mutation ArchiveTagMutation(\n  $tagId: ID!\n  $baseRevision: Int!\n  $archived: Boolean!\n) {\n  archiveTag(tagId: $tagId, baseRevision: $baseRevision, archived: $archived) {\n    __typename\n    ... on ArchiveTagSuccess {\n      result {\n        id\n        archived\n        revision\n      }\n    }\n    ... on MutationError {\n      code\n      message\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "656dd6670f26fb1de5cf7730a78ba5ba";

export default node;

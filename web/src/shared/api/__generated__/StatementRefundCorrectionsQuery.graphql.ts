/**
 * @generated SignedSource<<e2c2c0bbe57468eb48182ae1847b149e>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type StatementRefundCorrectionsQuery$variables = {
  after?: string | null | undefined;
  first?: number | null | undefined;
  householdId: string;
  sourceDocumentId: string;
  sourceReference: string;
};
export type StatementRefundCorrectionsQuery$data = {
  readonly statementRefundCorrections: {
    readonly edges: ReadonlyArray<{
      readonly cursor: string;
      readonly node: {
        readonly amountYen: any;
        readonly id: string;
        readonly merchant: string;
        readonly occurredAt: string;
        readonly sourceFilename: string;
      };
    }>;
    readonly pageInfo: {
      readonly endCursor: string | null | undefined;
      readonly hasNextPage: boolean;
    };
  };
};
export type StatementRefundCorrectionsQuery = {
  response: StatementRefundCorrectionsQuery$data;
  variables: StatementRefundCorrectionsQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "after"
},
v1 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "first"
},
v2 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "householdId"
},
v3 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "sourceDocumentId"
},
v4 = {
  "defaultValue": null,
  "kind": "LocalArgument",
  "name": "sourceReference"
},
v5 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "after",
        "variableName": "after"
      },
      {
        "kind": "Variable",
        "name": "first",
        "variableName": "first"
      },
      {
        "kind": "Variable",
        "name": "householdId",
        "variableName": "householdId"
      },
      {
        "kind": "Variable",
        "name": "sourceDocumentId",
        "variableName": "sourceDocumentId"
      },
      {
        "kind": "Variable",
        "name": "sourceReference",
        "variableName": "sourceReference"
      }
    ],
    "concreteType": "StatementRefundCorrectionConnection",
    "kind": "LinkedField",
    "name": "statementRefundCorrections",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "concreteType": "StatementRefundCorrectionEdge",
        "kind": "LinkedField",
        "name": "edges",
        "plural": true,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "cursor",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "concreteType": "StatementRefundCorrection",
            "kind": "LinkedField",
            "name": "node",
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
                "name": "amountYen",
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
                "name": "merchant",
                "storageKey": null
              },
              {
                "alias": null,
                "args": null,
                "kind": "ScalarField",
                "name": "sourceFilename",
                "storageKey": null
              }
            ],
            "storageKey": null
          }
        ],
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "concreteType": "PageInfo",
        "kind": "LinkedField",
        "name": "pageInfo",
        "plural": false,
        "selections": [
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "endCursor",
            "storageKey": null
          },
          {
            "alias": null,
            "args": null,
            "kind": "ScalarField",
            "name": "hasNextPage",
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
    "argumentDefinitions": [
      (v0/*:: as any*/),
      (v1/*:: as any*/),
      (v2/*:: as any*/),
      (v3/*:: as any*/),
      (v4/*:: as any*/)
    ],
    "kind": "Fragment",
    "metadata": null,
    "name": "StatementRefundCorrectionsQuery",
    "selections": (v5/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [
      (v2/*:: as any*/),
      (v3/*:: as any*/),
      (v4/*:: as any*/),
      (v1/*:: as any*/),
      (v0/*:: as any*/)
    ],
    "kind": "Operation",
    "name": "StatementRefundCorrectionsQuery",
    "selections": (v5/*:: as any*/)
  },
  "params": {
    "cacheID": "df39c1c969a07ef7f4504b5264f3a846",
    "id": null,
    "metadata": {},
    "name": "StatementRefundCorrectionsQuery",
    "operationKind": "query",
    "text": "query StatementRefundCorrectionsQuery(\n  $householdId: ID!\n  $sourceDocumentId: ID!\n  $sourceReference: String!\n  $first: Int\n  $after: String\n) {\n  statementRefundCorrections(householdId: $householdId, sourceDocumentId: $sourceDocumentId, sourceReference: $sourceReference, first: $first, after: $after) {\n    edges {\n      cursor\n      node {\n        id\n        amountYen\n        occurredAt\n        merchant\n        sourceFilename\n      }\n    }\n    pageInfo {\n      endCursor\n      hasNextPage\n    }\n  }\n}\n"
  }
};
})();

(node as any).hash = "489c0e0d25277cc54b83de6152da63cf";

export default node;

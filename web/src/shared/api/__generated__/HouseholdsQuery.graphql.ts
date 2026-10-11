/**
 * @generated SignedSource<<01f12ae854bb6c6c549e6b18baaffeb2>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type HouseholdsQuery$variables = Record<PropertyKey, never>;
export type HouseholdsQuery$data = {
  readonly households: ReadonlyArray<{
    readonly id: string;
    readonly name: string;
  }>;
};
export type HouseholdsQuery = {
  response: HouseholdsQuery$data;
  variables: HouseholdsQuery$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Household",
    "kind": "LinkedField",
    "name": "households",
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
        "name": "name",
        "storageKey": null
      }
    ],
    "storageKey": null
  }
];
return {
  "fragment": {
    "argumentDefinitions": [],
    "kind": "Fragment",
    "metadata": null,
    "name": "HouseholdsQuery",
    "selections": (v0/*:: as any*/),
    "type": "Query",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": [],
    "kind": "Operation",
    "name": "HouseholdsQuery",
    "selections": (v0/*:: as any*/)
  },
  "params": {
    "cacheID": "f73624e1e3e85884e2c52f71a00cee5f",
    "id": null,
    "metadata": {},
    "name": "HouseholdsQuery",
    "operationKind": "query",
    "text": "query HouseholdsQuery {\n  households {\n    id\n    name\n  }\n}\n"
  }
};
})();

(node as any).hash = "d39c5a89713d4d3d377ff7014e76970c";

export default node;

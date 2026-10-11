/**
 * @generated SignedSource<<4c05b9745df1649221348e40c905506a>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type SetProductAliasInput = {
  alias: string;
  householdId: string;
  merchant: string;
  variantId: string;
};
export type SetProductAliasMutation$variables = {
  input: SetProductAliasInput;
};
export type SetProductAliasMutation$data = {
  readonly setProductAlias: {
    readonly alias: string;
    readonly id: string;
    readonly merchantKey: string;
    readonly revision: number;
  };
};
export type SetProductAliasMutation = {
  response: SetProductAliasMutation$data;
  variables: SetProductAliasMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "input"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "input",
        "variableName": "input"
      }
    ],
    "concreteType": "ProductAlias",
    "kind": "LinkedField",
    "name": "setProductAlias",
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
        "name": "alias",
        "storageKey": null
      },
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "merchantKey",
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
    "name": "SetProductAliasMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "SetProductAliasMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "7c1124c2812f5dea3fc0d9939e449ba5",
    "id": null,
    "metadata": {},
    "name": "SetProductAliasMutation",
    "operationKind": "mutation",
    "text": "mutation SetProductAliasMutation(\n  $input: SetProductAliasInput!\n) {\n  setProductAlias(input: $input) {\n    id\n    alias\n    merchantKey\n    revision\n  }\n}\n"
  }
};
})();

(node as any).hash = "8a4bbee96569dbd68ed7248747ec7b59";

export default node;

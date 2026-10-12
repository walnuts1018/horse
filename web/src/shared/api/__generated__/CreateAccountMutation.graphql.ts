/**
 * @generated SignedSource<<7cf27771cdd68c19ed0cbb1e6676557d>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AccountKind = "BANK" | "CARD" | "CASH" | "OTHER" | "%future added value";
export type CreateAccountInput = {
  householdId: string;
  kind: AccountKind;
  name: string;
  openingBalanceAt?: string | null | undefined;
  openingBalanceYen?: any | null | undefined;
  poolId?: string | null | undefined;
};
export type CreateAccountMutation$variables = {
  input: CreateAccountInput;
};
export type CreateAccountMutation$data = {
  readonly createAccount: {
    readonly id: string;
  };
};
export type CreateAccountMutation = {
  response: CreateAccountMutation$data;
  variables: CreateAccountMutation$variables;
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
    "concreteType": "Account",
    "kind": "LinkedField",
    "name": "createAccount",
    "plural": false,
    "selections": [
      {
        "alias": null,
        "args": null,
        "kind": "ScalarField",
        "name": "id",
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
    "name": "CreateAccountMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "CreateAccountMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "b91b00cceaf61ad0e552bc7390968dab",
    "id": null,
    "metadata": {},
    "name": "CreateAccountMutation",
    "operationKind": "mutation",
    "text": "mutation CreateAccountMutation(\n  $input: CreateAccountInput!\n) {\n  createAccount(input: $input) {\n    id\n  }\n}\n"
  }
};
})();

(node as any).hash = "23a94d86c0819ed53a5c869eb2eb2fe6";

export default node;

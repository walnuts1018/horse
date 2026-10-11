/**
 * @generated SignedSource<<c5bc2223321ff6d74fa90210f4194f76>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ConcreteRequest } from 'relay-runtime';
export type AcceptHouseholdInvitationMutation$variables = {
  token: string;
};
export type AcceptHouseholdInvitationMutation$data = {
  readonly acceptHouseholdInvitation: {
    readonly id: string;
    readonly name: string;
  };
};
export type AcceptHouseholdInvitationMutation = {
  response: AcceptHouseholdInvitationMutation$data;
  variables: AcceptHouseholdInvitationMutation$variables;
};

const node: ConcreteRequest = (function(){
var v0 = [
  {
    "defaultValue": null,
    "kind": "LocalArgument",
    "name": "token"
  }
],
v1 = [
  {
    "alias": null,
    "args": [
      {
        "kind": "Variable",
        "name": "token",
        "variableName": "token"
      }
    ],
    "concreteType": "Household",
    "kind": "LinkedField",
    "name": "acceptHouseholdInvitation",
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
        "name": "name",
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
    "name": "AcceptHouseholdInvitationMutation",
    "selections": (v1/*:: as any*/),
    "type": "Mutation",
    "abstractKey": null
  },
  "kind": "Request",
  "operation": {
    "argumentDefinitions": (v0/*:: as any*/),
    "kind": "Operation",
    "name": "AcceptHouseholdInvitationMutation",
    "selections": (v1/*:: as any*/)
  },
  "params": {
    "cacheID": "af9a9a7049b78fcf06ffa966c64b133f",
    "id": null,
    "metadata": {},
    "name": "AcceptHouseholdInvitationMutation",
    "operationKind": "mutation",
    "text": "mutation AcceptHouseholdInvitationMutation(\n  $token: String!\n) {\n  acceptHouseholdInvitation(token: $token) {\n    id\n    name\n  }\n}\n"
  }
};
})();

(node as any).hash = "1f600bf10ed49e67d440af63c9ffc261";

export default node;

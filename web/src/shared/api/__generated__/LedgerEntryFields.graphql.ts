/**
 * @generated SignedSource<<288df1d830554e666d1455cdb66f8db3>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
import type { FragmentRefs } from "relay-runtime";
export type LedgerEntryFields$data = {
  readonly __typename: string;
  readonly amountYen: any;
  readonly createdAt: string;
  readonly currency: string;
  readonly description: string;
  readonly fromMember?: {
    readonly displayName: string;
    readonly id: string;
  };
  readonly id: string;
  readonly loan?: {
    readonly id: string;
  };
  readonly member?: {
    readonly displayName: string;
    readonly id: string;
  };
  readonly occurredAt: string;
  readonly pool?: {
    readonly id: string;
    readonly name: string;
  };
  readonly purchase?: {
    readonly id: string;
  };
  readonly toMember?: {
    readonly displayName: string;
    readonly id: string;
  };
  readonly " $fragmentType": "LedgerEntryFields";
};
export type LedgerEntryFields$key = {
  readonly " $data"?: LedgerEntryFields$data;
  readonly " $fragmentSpreads": FragmentRefs<"LedgerEntryFields">;
};

const node: ReaderFragment = (function(){
var v0 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "id",
  "storageKey": null
},
v1 = [
  (v0/*:: as any*/)
],
v2 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "purchase",
    "plural": false,
    "selections": (v1/*:: as any*/),
    "storageKey": null
  }
],
v3 = [
  (v0/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "displayName",
    "storageKey": null
  }
],
v4 = [
  {
    "alias": null,
    "args": null,
    "concreteType": "LoanBalance",
    "kind": "LinkedField",
    "name": "loan",
    "plural": false,
    "selections": (v1/*:: as any*/),
    "storageKey": null
  }
];
return {
  "argumentDefinitions": [],
  "kind": "Fragment",
  "metadata": null,
  "name": "LedgerEntryFields",
  "selections": [
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "__typename",
      "storageKey": null
    },
    (v0/*:: as any*/),
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
      "name": "description",
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
      "name": "currency",
      "storageKey": null
    },
    {
      "alias": null,
      "args": null,
      "kind": "ScalarField",
      "name": "createdAt",
      "storageKey": null
    },
    {
      "kind": "InlineFragment",
      "selections": (v2/*:: as any*/),
      "type": "PurchaseLedgerEntry",
      "abstractKey": null
    },
    {
      "kind": "InlineFragment",
      "selections": [
        {
          "alias": null,
          "args": null,
          "concreteType": "Member",
          "kind": "LinkedField",
          "name": "member",
          "plural": false,
          "selections": (v3/*:: as any*/),
          "storageKey": null
        },
        {
          "alias": null,
          "args": null,
          "concreteType": null,
          "kind": "LinkedField",
          "name": "pool",
          "plural": false,
          "selections": [
            (v0/*:: as any*/),
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
      ],
      "type": "FundingLedgerEntry",
      "abstractKey": null
    },
    {
      "kind": "InlineFragment",
      "selections": [
        {
          "alias": null,
          "args": null,
          "concreteType": "Member",
          "kind": "LinkedField",
          "name": "fromMember",
          "plural": false,
          "selections": (v3/*:: as any*/),
          "storageKey": null
        },
        {
          "alias": null,
          "args": null,
          "concreteType": "Member",
          "kind": "LinkedField",
          "name": "toMember",
          "plural": false,
          "selections": (v3/*:: as any*/),
          "storageKey": null
        }
      ],
      "type": "TransferLedgerEntry",
      "abstractKey": "__isTransferLedgerEntry"
    },
    {
      "kind": "InlineFragment",
      "selections": (v4/*:: as any*/),
      "type": "LoanTransferLedgerEntry",
      "abstractKey": null
    },
    {
      "kind": "InlineFragment",
      "selections": (v4/*:: as any*/),
      "type": "RepaymentTransferLedgerEntry",
      "abstractKey": null
    },
    {
      "kind": "InlineFragment",
      "selections": (v2/*:: as any*/),
      "type": "CorrectionLedgerEntry",
      "abstractKey": null
    }
  ],
  "type": "LedgerEntry",
  "abstractKey": "__isLedgerEntry"
};
})();

(node as any).hash = "23c32b2fb1af7946beb0d1c26367ed73";

export default node;

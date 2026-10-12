/**
 * @generated SignedSource<<bb661c1bb8b1a85fb8c8e35ebdd8e999>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
export type TransferKind = "ADVANCE" | "GIFT" | "LOAN" | "REPAYMENT" | "%future added value";
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
    readonly loanId: string;
  } | null | undefined;
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
  readonly transferKind?: TransferKind;
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
  {
    "alias": null,
    "args": null,
    "concreteType": "Purchase",
    "kind": "LinkedField",
    "name": "purchase",
    "plural": false,
    "selections": [
      (v0/*:: as any*/)
    ],
    "storageKey": null
  }
],
v2 = [
  (v0/*:: as any*/),
  {
    "alias": null,
    "args": null,
    "kind": "ScalarField",
    "name": "displayName",
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
      "selections": (v1/*:: as any*/),
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
          "selections": (v2/*:: as any*/),
          "storageKey": null
        },
        {
          "alias": null,
          "args": null,
          "concreteType": "Pool",
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
          "selections": (v2/*:: as any*/),
          "storageKey": null
        },
        {
          "alias": null,
          "args": null,
          "concreteType": "Member",
          "kind": "LinkedField",
          "name": "toMember",
          "plural": false,
          "selections": (v2/*:: as any*/),
          "storageKey": null
        },
        {
          "alias": null,
          "args": null,
          "kind": "ScalarField",
          "name": "transferKind",
          "storageKey": null
        },
        {
          "alias": null,
          "args": null,
          "concreteType": "LoanBalance",
          "kind": "LinkedField",
          "name": "loan",
          "plural": false,
          "selections": [
            {
              "alias": null,
              "args": null,
              "kind": "ScalarField",
              "name": "loanId",
              "storageKey": null
            }
          ],
          "storageKey": null
        }
      ],
      "type": "TransferLedgerEntry",
      "abstractKey": null
    },
    {
      "kind": "InlineFragment",
      "selections": (v1/*:: as any*/),
      "type": "CorrectionLedgerEntry",
      "abstractKey": null
    }
  ],
  "type": "LedgerEntry",
  "abstractKey": "__isLedgerEntry"
};
})();

(node as any).hash = "8e0d60c635a8609f28033bc596eb11fd";

export default node;

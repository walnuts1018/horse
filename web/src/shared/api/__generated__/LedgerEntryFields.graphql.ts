/**
 * @generated SignedSource<<0c4c83e6fded3c9ecebe5419a96fc4cc>>
 * @lightSyntaxTransform
 */

/* tslint:disable */
/* eslint-disable */
// @ts-nocheck

import type { ReaderFragment } from 'relay-runtime';
export type TransferKind = "ADVANCE" | "GIFT" | "LOAN" | "REPAYMENT" | "%future added value";
import type { FragmentRefs } from "relay-runtime";
export type LedgerEntryFields$data = {
  readonly __typename: "CorrectionLedgerEntry";
  readonly amountYen: any;
  readonly createdAt: string;
  readonly currency: string;
  readonly description: string;
  readonly id: string;
  readonly occurredAt: string;
  readonly purchase: {
    readonly id: string;
  };
  readonly " $fragmentType": "LedgerEntryFields";
} | {
  readonly __typename: "FundingLedgerEntry";
  readonly amountYen: any;
  readonly createdAt: string;
  readonly currency: string;
  readonly description: string;
  readonly id: string;
  readonly member: {
    readonly displayName: string;
    readonly id: string;
  };
  readonly occurredAt: string;
  readonly pool: {
    readonly id: string;
    readonly name: string;
  };
  readonly " $fragmentType": "LedgerEntryFields";
} | {
  readonly __typename: "PurchaseLedgerEntry";
  readonly amountYen: any;
  readonly createdAt: string;
  readonly currency: string;
  readonly description: string;
  readonly id: string;
  readonly occurredAt: string;
  readonly purchase: {
    readonly id: string;
  };
  readonly " $fragmentType": "LedgerEntryFields";
} | {
  readonly __typename: "TransferLedgerEntry";
  readonly amountYen: any;
  readonly createdAt: string;
  readonly currency: string;
  readonly description: string;
  readonly fromMember: {
    readonly displayName: string;
    readonly id: string;
  };
  readonly id: string;
  readonly loan: {
    readonly loanId: string;
  } | null | undefined;
  readonly occurredAt: string;
  readonly toMember: {
    readonly displayName: string;
    readonly id: string;
  };
  readonly transferKind: TransferKind;
  readonly " $fragmentType": "LedgerEntryFields";
} | {
  // This will never be '%other', but we need some
  // value in case none of the concrete values match.
  readonly __typename: "%other";
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
v1 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "occurredAt",
  "storageKey": null
},
v2 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "description",
  "storageKey": null
},
v3 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "amountYen",
  "storageKey": null
},
v4 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "currency",
  "storageKey": null
},
v5 = {
  "alias": null,
  "args": null,
  "kind": "ScalarField",
  "name": "createdAt",
  "storageKey": null
},
v6 = [
  (v0/*:: as any*/),
  (v1/*:: as any*/),
  (v2/*:: as any*/),
  (v3/*:: as any*/),
  (v4/*:: as any*/),
  (v5/*:: as any*/),
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
v7 = [
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
    {
      "kind": "InlineFragment",
      "selections": (v6/*:: as any*/),
      "type": "PurchaseLedgerEntry",
      "abstractKey": null
    },
    {
      "kind": "InlineFragment",
      "selections": [
        (v0/*:: as any*/),
        (v1/*:: as any*/),
        (v2/*:: as any*/),
        (v3/*:: as any*/),
        (v4/*:: as any*/),
        (v5/*:: as any*/),
        {
          "alias": null,
          "args": null,
          "concreteType": "Member",
          "kind": "LinkedField",
          "name": "member",
          "plural": false,
          "selections": (v7/*:: as any*/),
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
        (v0/*:: as any*/),
        (v1/*:: as any*/),
        (v2/*:: as any*/),
        (v3/*:: as any*/),
        (v4/*:: as any*/),
        (v5/*:: as any*/),
        {
          "alias": null,
          "args": null,
          "concreteType": "Member",
          "kind": "LinkedField",
          "name": "fromMember",
          "plural": false,
          "selections": (v7/*:: as any*/),
          "storageKey": null
        },
        {
          "alias": null,
          "args": null,
          "concreteType": "Member",
          "kind": "LinkedField",
          "name": "toMember",
          "plural": false,
          "selections": (v7/*:: as any*/),
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
      "selections": (v6/*:: as any*/),
      "type": "CorrectionLedgerEntry",
      "abstractKey": null
    }
  ],
  "type": "LedgerEntry",
  "abstractKey": "__isLedgerEntry"
};
})();

(node as any).hash = "58c1e92010602de98f852f14592e6aa4";

export default node;

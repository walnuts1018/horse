import { useCallback, useRef, useState } from "react";
import { fetchQuery } from "react-relay";

import { mapPurchase, purchaseHistoryQuery } from "../../../entities/purchase";
import {
  getRelayEnvironment,
  type Household,
  type Purchase,
  type PurchaseHistoryQuery,
  type StatementRefundCorrectionsQuery,
} from "../../../shared/api";
import { statementRefundCorrectionsQuery } from "../api/operations";
import { statementSourceReference, type StatementEntry } from "../model/imports";

export function useStatementRefundCandidateData({
  documentId,
  household,
}: {
  documentId: string;
  household: Household;
}) {
  const [refundTargets, setRefundTargets] = useState<
    Record<string, { purchaseId: string; itemId: string }>
  >({});
  const [refundPurchaseCandidates, setRefundPurchaseCandidates] = useState<
    Record<string, Purchase[]>
  >({});
  const [refundPurchaseCandidateState, setRefundPurchaseCandidateState] = useState<
    Record<string, { loading: boolean; error: boolean }>
  >({});
  const loadedRefundPurchaseCandidates = useRef(new Set<string>());
  const [refundCorrectionTargets, setRefundCorrectionTargets] = useState<Record<string, string>>(
    {},
  );
  const [openRefundCorrections, setOpenRefundCorrections] = useState<Record<string, boolean>>({});
  const [refundCorrectionCandidates, setRefundCorrectionCandidates] = useState<
    Record<
      string,
      StatementRefundCorrectionsQuery["response"]["statementRefundCorrections"]["edges"]
    >
  >({});
  const [refundCorrectionPages, setRefundCorrectionPages] = useState<
    Record<string, { after: string | null; hasNextPage: boolean; loading: boolean }>
  >({});
  const [refundCorrectionErrors, setRefundCorrectionErrors] = useState<Record<string, string>>({});
  const loadRefundCorrections = (entry: StatementEntry, after?: string) => {
    const reference = statementSourceReference(entry);
    const key = `${documentId}:${reference}`;
    setRefundCorrectionErrors((current) => ({ ...current, [key]: "" }));
    setRefundCorrectionPages((current) => ({
      ...current,
      [key]: {
        after: current[key]?.after ?? null,
        hasNextPage: false,
        loading: true,
      },
    }));
    fetchQuery<StatementRefundCorrectionsQuery>(
      getRelayEnvironment(),
      statementRefundCorrectionsQuery,
      {
        householdId: household.id,
        sourceDocumentId: documentId,
        sourceReference: reference,
        first: 50,
        after: after ?? null,
      },
    ).subscribe({
      next: (data) => {
        const page = data.statementRefundCorrections;
        setRefundCorrectionCandidates((current) => ({
          ...current,
          [key]: after ? [...(current[key] ?? []), ...page.edges] : page.edges,
        }));
        setRefundCorrectionPages((current) => ({
          ...current,
          [key]: {
            after: page.pageInfo.endCursor ?? null,
            hasNextPage: page.pageInfo.hasNextPage,
            loading: false,
          },
        }));
      },
      error: () => {
        setRefundCorrectionPages((current) => ({
          ...current,
          [key]: {
            after: current[key]?.after ?? null,
            hasNextPage: false,
            loading: false,
          },
        }));
        setRefundCorrectionErrors((current) => ({
          ...current,
          [key]: "返金記録を読み込めませんでした",
        }));
      },
    });
  };
  const toggleRefundCorrections = (entry: StatementEntry) => {
    const key = `${documentId}:${statementSourceReference(entry)}`;
    if (openRefundCorrections[key]) {
      setOpenRefundCorrections((current) => ({ ...current, [key]: false }));
      return;
    }
    setOpenRefundCorrections((current) => ({ ...current, [key]: true }));
    if (!(key in refundCorrectionCandidates)) loadRefundCorrections(entry);
  };
  const loadRefundPurchaseCandidates = useCallback(
    (entry: StatementEntry) => {
      const reference = statementSourceReference(entry);
      const key = `${household.id}:${documentId}:${reference}`;
      if (loadedRefundPurchaseCandidates.current.has(key)) return;
      loadedRefundPurchaseCandidates.current.add(key);
      setRefundPurchaseCandidateState((current) => ({
        ...current,
        [key]: { loading: true, error: false },
      }));
      const amount = Math.abs(entry.amount);
      const filters = ["PURCHASE", "ITEM"] as const;
      const searchCandidates = async (search?: string) => {
        const results = await Promise.all(
          filters.map((amountTarget) =>
            fetchQuery<PurchaseHistoryQuery>(getRelayEnvironment(), purchaseHistoryQuery, {
              filter: {
                householdId: household.id,
                search,
                minimumAmount: amount,
                maximumAmount: amount,
                amountTarget,
              },
              first: 100,
              after: null,
            }).toPromise(),
          ),
        );
        const candidates = new Map<string, Purchase>();
        for (const result of results) {
          for (const { node } of result?.purchases.edges ?? []) {
            if (node.status !== "DRAFT") candidates.set(node.id, mapPurchase(node));
          }
        }
        return [...candidates.values()];
      };
      searchCandidates(entry.merchant.trim() || undefined)
        .then((candidates) =>
          candidates.length || !entry.merchant.trim() ? candidates : searchCandidates(),
        )
        .then((candidates) => {
          setRefundPurchaseCandidates((current) => ({
            ...current,
            [key]: candidates,
          }));
          setRefundPurchaseCandidateState((current) => ({
            ...current,
            [key]: { loading: false, error: false },
          }));
        })
        .catch(() => {
          loadedRefundPurchaseCandidates.current.delete(key);
          setRefundPurchaseCandidateState((current) => ({
            ...current,
            [key]: { loading: false, error: true },
          }));
        });
    },
    [documentId, household.id],
  );

  return {
    refundTargets,
    setRefundTargets,
    refundPurchaseCandidates,
    refundPurchaseCandidateState,
    refundCorrectionTargets,
    setRefundCorrectionTargets,
    openRefundCorrections,
    refundCorrectionCandidates,
    refundCorrectionPages,
    refundCorrectionErrors,
    loadRefundCorrections,
    toggleRefundCorrections,
    loadRefundPurchaseCandidates,
  };
}

import { useEffect, useMemo, useState } from "react";

import { readProductSuggestions, type ProductSuggestion } from "../../../entities/product";

export function useProductSuggestions({
  householdId,
  query,
  merchant,
  limit,
  enabled = true,
  delay = 180,
}: {
  householdId: string;
  query: string;
  merchant: string;
  limit: number;
  enabled?: boolean;
  delay?: number;
}) {
  const [revision, setRevision] = useState(0);
  const request = useMemo(() => {
    const search = query.trim();
    const normalizedMerchant = merchant.trim() || null;
    return {
      householdId,
      search,
      merchant: normalizedMerchant,
      limit,
      key: JSON.stringify([householdId, search, normalizedMerchant, limit]),
    };
  }, [householdId, limit, merchant, query]);
  const [result, setResult] = useState<{
    key: string;
    suggestions: ProductSuggestion[];
    error: boolean;
  } | null>(null);
  const [loadingKey, setLoadingKey] = useState("");

  useEffect(() => {
    if (!enabled || request.search.length < 2) return;

    let active = true;
    const loadingRequestKey = `${request.key}:${revision}`;
    const timer = window.setTimeout(() => {
      setLoadingKey(loadingRequestKey);
      void readProductSuggestions(
        request.householdId,
        request.search,
        request.merchant,
        request.limit,
      )
        .then((suggestions) => {
          if (active) setResult({ key: request.key, suggestions, error: false });
        })
        .catch(() => {
          if (active) setResult({ key: request.key, suggestions: [], error: true });
        })
        .finally(() => {
          if (active) setLoadingKey((current) => (current === loadingRequestKey ? "" : current));
        });
    }, delay);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [delay, enabled, request, revision]);

  return {
    suggestions: result?.key === request.key ? result.suggestions : [],
    loading:
      enabled &&
      request.search.length >= 2 &&
      (loadingKey === `${request.key}:${revision}` || result?.key !== request.key),
    error: result?.key === request.key && result.error,
    retry: () => setRevision((current) => current + 1),
  };
}

import { useCallback, useEffect, useRef, useState } from "react";

import type { AppPage } from "../app-page";

import { readHousehold, readHouseholds } from "../../entities/household";
import { ApiError, getViewer, type Household } from "../../shared/api";
import { errorMessage, toError } from "../../shared/lib";

type Viewer = { id: string; displayName: string; email?: string };
type HouseholdSummary = { id: string; name: string };

export function useHouseholdSession({
  householdId,
  page,
  navigateToHouseholdPage,
}: {
  householdId: string | undefined;
  page: AppPage;
  navigateToHouseholdPage: (id: string, page: AppPage, replace?: boolean) => void;
}) {
  const [viewer, setViewer] = useState<Viewer | null>(null);
  const [households, setHouseholds] = useState<HouseholdSummary[]>([]);
  const [household, setHousehold] = useState<Household | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [householdLoadErrorState, setHouseholdLoadErrorState] = useState<{
    id: string;
    message: string;
  } | null>(null);
  const householdRequestSequence = useRef(0);
  const loadingHouseholdId = useRef<string | undefined>(undefined);
  const hasRequestedHousehold = households.some((item) => item.id === householdId);
  const householdLoading = Boolean(
    householdId &&
    hasRequestedHousehold &&
    household?.id !== householdId &&
    householdLoadErrorState?.id !== householdId,
  );
  const householdLoadError =
    householdLoadErrorState && householdLoadErrorState.id === householdId
      ? householdLoadErrorState.message
      : "";
  const redirectHouseholdId =
    households.length > 0 && !hasRequestedHousehold ? households[0].id : undefined;

  const loadHousehold = useCallback((id: string, clearError = true) => {
    const requestSequence = ++householdRequestSequence.current;
    loadingHouseholdId.current = id;
    if (clearError) setHouseholdLoadErrorState(null);
    void readHousehold(id)
      .then((data) => {
        if (householdRequestSequence.current !== requestSequence) return;
        loadingHouseholdId.current = undefined;
        setHousehold(data);
        setHouseholdLoadErrorState(null);
      })
      .catch((reason: unknown) => {
        if (householdRequestSequence.current !== requestSequence) return;
        loadingHouseholdId.current = undefined;
        setHouseholdLoadErrorState({
          id,
          message: errorMessage(reason) || "家計簿を読み込めませんでした",
        });
      });
    return requestSequence;
  }, []);

  const cancelHouseholdRequest = useCallback((requestSequence: number) => {
    if (householdRequestSequence.current !== requestSequence) return;
    householdRequestSequence.current++;
    loadingHouseholdId.current = undefined;
  }, []);

  useEffect(() => {
    let active = true;
    void Promise.all([getViewer(), readHouseholds()])
      .then(([session, householdList]) => {
        if (!active) return;
        setViewer(session);
        setHouseholds(householdList);
      })
      .catch((reason: unknown) => {
        if (!active) return;
        const err = toError(reason);
        if (err instanceof ApiError && err.status === 401) setError("ログインして家計簿を開く");
        else setError(err.message || "データを読み込めませんでした");
      })
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!households.length) return;
    const requestedHousehold = households.find((item) => item.id === householdId);
    if (!requestedHousehold) return;
    if (household?.id === requestedHousehold.id) return;
    if (householdLoadErrorState?.id === requestedHousehold.id) return;
    if (loadingHouseholdId.current === requestedHousehold.id) return;
    const requestSequence = loadHousehold(requestedHousehold.id, false);
    return () => cancelHouseholdRequest(requestSequence);
  }, [
    households,
    householdId,
    household?.id,
    householdLoadErrorState,
    loadHousehold,
    cancelHouseholdRequest,
  ]);

  const refreshHousehold = useCallback(async (id: string) => {
    setHousehold(await readHousehold(id));
  }, []);

  const retryHousehold = useCallback(() => {
    if (householdId && hasRequestedHousehold) loadHousehold(householdId);
  }, [householdId, hasRequestedHousehold, loadHousehold]);

  const selectHousehold = useCallback(
    (id: string) => {
      if (!households.some((item) => item.id === id)) return;
      if (id === household?.id && id === householdId) return;
      if (id === householdId) {
        loadHousehold(id);
        return;
      }
      navigateToHouseholdPage(id, page);
    },
    [households, household?.id, householdId, navigateToHouseholdPage, page, loadHousehold],
  );

  return {
    viewer,
    households,
    setHouseholds,
    household,
    redirectHouseholdId,
    loading,
    error,
    householdLoading,
    householdLoadError,
    refreshHousehold,
    retryHousehold,
    selectHousehold,
  };
}

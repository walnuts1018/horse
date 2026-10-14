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
  currentPage,
  navigateToHouseholdPage,
  showNotice,
}: {
  householdId: string | undefined;
  page: AppPage;
  currentPage: () => AppPage;
  navigateToHouseholdPage: (id: string, page: AppPage, replace?: boolean) => void;
  showNotice: (message: string, kind?: "info" | "error" | "success") => void;
}) {
  const [viewer, setViewer] = useState<Viewer | null>(null);
  const [households, setHouseholds] = useState<HouseholdSummary[]>([]);
  const [household, setHousehold] = useState<Household | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [loadingHouseholdId, setLoadingHouseholdId] = useState<string>();
  const currentHouseholdId = useRef(household?.id);
  const hasRequestedHousehold = households.some((item) => item.id === householdId);
  const householdLoading = Boolean(
    householdId &&
    hasRequestedHousehold &&
    loadingHouseholdId === householdId &&
    household?.id !== householdId,
  );
  const redirectHouseholdId =
    households.length > 0 && !hasRequestedHousehold ? households[0].id : undefined;

  useEffect(() => {
    currentHouseholdId.current = household?.id;
  }, [household?.id]);

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
    if (currentHouseholdId.current === requestedHousehold.id) return;
    let active = true;
    setLoadingHouseholdId(householdId);
    void readHousehold(requestedHousehold.id)
      .then((data) => {
        if (active) setHousehold(data);
      })
      .catch((reason: unknown) => {
        if (!active) return;
        showNotice(errorMessage(reason) || "家計簿を読み込めませんでした", "error");
        const currentId = currentHouseholdId.current;
        if (currentId && currentId !== requestedHousehold.id)
          navigateToHouseholdPage(currentId, currentPage(), true);
      })
      .finally(() => {
        if (active)
          setLoadingHouseholdId((current) =>
            current === requestedHousehold.id ? undefined : current,
          );
      });
    return () => {
      active = false;
    };
  }, [households, householdId, navigateToHouseholdPage, currentPage, showNotice]);

  const refreshHousehold = useCallback(async (id: string) => {
    setHousehold(await readHousehold(id));
  }, []);

  const selectHousehold = useCallback(
    (id: string) => {
      if (!households.some((item) => item.id === id)) return;
      if (id === household?.id && id === householdId) return;
      if (id === householdId) {
        setLoadingHouseholdId(id);
        void readHousehold(id)
          .then(setHousehold)
          .catch((reason: unknown) => {
            showNotice(errorMessage(reason) || "家計簿を読み込めませんでした", "error");
          })
          .finally(() =>
            setLoadingHouseholdId((current) => (current === id ? undefined : current)),
          );
        return;
      }
      navigateToHouseholdPage(id, page);
    },
    [households, household?.id, householdId, navigateToHouseholdPage, page, showNotice],
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
    refreshHousehold,
    selectHousehold,
  };
}

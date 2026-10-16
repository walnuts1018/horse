import { useEffect, useRef, useSyncExternalStore } from "react";

export type SettingsTab = "accounts" | "allocation" | "catalog" | "planning" | "members";
export const defaultSettingsTab: SettingsTab = "accounts";

export function isSettingsTab(value: unknown): value is SettingsTab {
  return (
    value === "accounts" ||
    value === "allocation" ||
    value === "catalog" ||
    value === "planning" ||
    value === "members"
  );
}

export const settingsTabs: { id: SettingsTab; label: string }[] = [
  { id: "accounts", label: "口座" },
  { id: "allocation", label: "負担" },
  { id: "catalog", label: "分類" },
  { id: "planning", label: "予算・予定" },
  { id: "members", label: "メンバー" },
];

const settingsTabByHash: Record<string, SettingsTab> = {
  "pool-settings": "accounts",
  "payment-source-settings": "accounts",
  "default-allocation-settings": "allocation",
};

function getTabFromHash(hash: string): SettingsTab | undefined {
  return settingsTabByHash[hash];
}

const getSetupHashSnapshot = () => window.location.hash.slice(1);
const getSetupHashServerSnapshot = () => "";
const subscribeSetupHash = (onStoreChange: () => void) => {
  window.addEventListener("hashchange", onStoreChange);
  return () => window.removeEventListener("hashchange", onStoreChange);
};

export function useSettingsNavigation(
  activeSettingsTab: SettingsTab,
  selectTab: (tab: SettingsTab) => void,
) {
  const setupHashTarget = useSyncExternalStore(
    subscribeSetupHash,
    getSetupHashSnapshot,
    getSetupHashServerSnapshot,
  );
  const appliedHash = useRef<string | null>(null);

  useEffect(() => {
    if (appliedHash.current === setupHashTarget) return;
    appliedHash.current = setupHashTarget;
    const tab = getTabFromHash(setupHashTarget);
    if (tab !== undefined && tab !== activeSettingsTab) selectTab(tab);
  }, [setupHashTarget, activeSettingsTab, selectTab]);

  return { activeSettingsTab, selectTab };
}

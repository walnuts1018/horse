import { useEffect, useState, useSyncExternalStore } from "react";

export type SettingsTab = "accounts" | "allocation" | "catalog" | "planning" | "members";

export const settingsTabs: { id: SettingsTab; label: string }[] = [
  { id: "accounts", label: "口座" },
  { id: "allocation", label: "負担" },
  { id: "catalog", label: "分類" },
  { id: "planning", label: "予算" },
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

export function useSettingsNavigation() {
  const [activeSettingsTab, setActiveSettingsTab] = useState<SettingsTab>("accounts");
  const setupHashTarget = useSyncExternalStore(
    subscribeSetupHash,
    getSetupHashSnapshot,
    getSetupHashServerSnapshot,
  );

  useEffect(() => {
    const tab = getTabFromHash(setupHashTarget);
    if (tab === undefined || tab === activeSettingsTab) return;
    queueMicrotask(() => setActiveSettingsTab(tab));
  }, [setupHashTarget, activeSettingsTab]);

  const selectTab = (tab: SettingsTab) => {
    setActiveSettingsTab(tab);
  };

  return { activeSettingsTab, selectTab };
}

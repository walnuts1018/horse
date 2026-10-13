import { useEffect, useState } from "react";

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

export function useSettingsNavigation() {
  const [activeSettingsTab, setActiveSettingsTab] = useState<SettingsTab>("accounts");
  const [setupHashTarget, setSetupHashTarget] = useState<string | null>(null);

  useEffect(() => {
    const selectSettingsTabFromHash = () => {
      const sectionId = window.location.hash.slice(1);
      const tab = settingsTabByHash[sectionId];
      if (!tab) {
        setSetupHashTarget(null);
        return;
      }
      setActiveSettingsTab(tab);
      setSetupHashTarget(sectionId);
    };
    selectSettingsTabFromHash();
    window.addEventListener("hashchange", selectSettingsTabFromHash);
    return () => window.removeEventListener("hashchange", selectSettingsTabFromHash);
  }, []);

  useEffect(() => {
    if (!setupHashTarget || settingsTabByHash[setupHashTarget] !== activeSettingsTab) return;
    document.getElementById(setupHashTarget)?.scrollIntoView({ block: "start" });
    setSetupHashTarget(null);
  }, [activeSettingsTab, setupHashTarget]);

  const selectTab = (tab: SettingsTab) => {
    setActiveSettingsTab(tab);
    setSetupHashTarget(null);
  };

  return { activeSettingsTab, selectTab };
}

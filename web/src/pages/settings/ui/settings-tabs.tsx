import { settingsTabs, type SettingsTab } from "../model/settings-navigation";

import styles from "./settings-page.module.css";

export function SettingsTabs({
  activeTab,
  onSelect,
}: {
  activeTab: SettingsTab;
  onSelect: (tab: SettingsTab) => void;
}) {
  return (
    <nav aria-label="設定項目" className={styles.settingsTabs}>
      {settingsTabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          className={`button ${activeTab === tab.id ? "button-primary" : "button-soft"}`}
          aria-pressed={activeTab === tab.id}
          onClick={() => onSelect(tab.id)}
        >
          {tab.label}
        </button>
      ))}
    </nav>
  );
}

import { settingsTabs, type SettingsTab } from "../model/settings-navigation";

export function SettingsTabs({
  activeTab,
  onSelect,
}: {
  activeTab: SettingsTab;
  onSelect: (tab: SettingsTab) => void;
}) {
  return (
    <nav
      aria-label="設定項目"
      style={{ display: "flex", flexWrap: "wrap", gap: 8, marginBottom: 14 }}
    >
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

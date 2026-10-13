import type { Household } from "../../../shared/api";
import styles from "./settings-page.module.css";

export function SettingsMembersSection({
  members,
  hidden,
}: {
  members: Household["members"];
  hidden: boolean;
}) {
  return (
    <section className={`panel ${styles.settingsSection} ${styles.memberSection}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">共同編集</span>
          <h2>メンバー</h2>
        </div>
      </div>
      {members.map((member) => (
        <div className={styles.settingRow} key={member.id}>
          <div className="profile-avatar">{member.displayName.slice(0, 1)}</div>
          <div>
            <b>{member.displayName}</b>
            <small>{member.role === "OWNER" ? "オーナー" : "メンバー"}</small>
          </div>
        </div>
      ))}
    </section>
  );
}

import { AlertCircle, Check, Info, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";
import styles from "./notice.module.css";

export type NoticeKind = "success" | "error" | "warning" | "info";

const noticeIcons: Record<NoticeKind, ReactNode> = {
  success: <Check size={17} />,
  error: <AlertCircle size={17} />,
  warning: <TriangleAlert size={17} />,
  info: <Info size={17} />,
};

export function Notice({ message, kind }: { message: string; kind: NoticeKind }) {
  const urgent = kind === "error" || kind === "warning";
  return (
    <div
      className={`toast ${styles.notice} ${styles[kind]}`}
      role={urgent ? "alert" : "status"}
      aria-live={urgent ? "assertive" : "polite"}
      aria-atomic="true"
    >
      <span className={styles.icon} aria-hidden="true">
        {noticeIcons[kind]}
      </span>
      <span>{message}</span>
    </div>
  );
}

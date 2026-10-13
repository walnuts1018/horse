import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";

function InlineError({ message }: { message: string }) {
  return (
    <div className="inline-error">
      <span>!</span>
      {message}
    </div>
  );
}
function EmptyState({
  icon,
  title,
  action,
  onClick,
  actionLink,
}: {
  icon: ReactNode;
  title: string;
  action: string;
  onClick?: () => void;
  actionLink?: ReactNode;
}) {
  return (
    <div className="empty-state">
      {icon}
      <span>{title}</span>
      {actionLink ?? (
        <button className="text-button" onClick={onClick}>
          {action} <ChevronRight size={15} />
        </button>
      )}
    </div>
  );
}

export { InlineError, EmptyState };

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
}: {
  icon: ReactNode;
  title: string;
  action: string;
  onClick: () => void;
}) {
  return (
    <div className="empty-state">
      {icon}
      <span>{title}</span>
      <button className="text-button" onClick={onClick}>
        {action} <ChevronRight size={15} />
      </button>
    </div>
  );
}

export { InlineError, EmptyState };

import { InlineError } from "../../../shared/ui";

export function SettlementFeedback({
  error,
  success,
  loadError,
  onRetryLoad,
}: {
  error: string;
  success: string;
  loadError: string;
  onRetryLoad: () => void;
}) {
  if (!error && !success && !loadError) return null;

  return (
    <>
      {error && <InlineError message={error} />}
      {success && (
        <p className="settlement-confirmation" role="status">
          {success}
        </p>
      )}
      {loadError && (
        <div className="settlement-load-error">
          <InlineError message={loadError} />
          <button className="text-button" type="button" onClick={onRetryLoad}>
            再試行
          </button>
        </div>
      )}
    </>
  );
}

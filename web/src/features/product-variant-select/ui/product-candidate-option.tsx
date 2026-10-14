export function ProductCandidateOption({
  label,
  description,
  badge,
  meta,
}: {
  label: string;
  description?: string;
  badge?: string;
  meta?: string;
}) {
  return (
    <span className="product-candidate-option">
      <span className="product-candidate-option__label">{label}</span>
      {description && (
        <small className="product-candidate-option__description">{description}</small>
      )}
      {meta && <small className="product-candidate-option__meta">{meta}</small>}
      {badge && <span className="product-candidate-option__badge">{badge}</span>}
    </span>
  );
}

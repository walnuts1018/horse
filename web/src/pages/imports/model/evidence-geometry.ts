export type Region = { page: number; top: number; left: number; bottom: number; right: number };
export type PreviewTransform = [
  number,
  number,
  number,
  number,
  number,
  number,
  number,
  number,
  number,
];

export const identityTransform: PreviewTransform = [1, 0, 0, 0, 1, 0, 0, 0, 1];

export const invertTransform = (matrix: PreviewTransform): PreviewTransform | null => {
  const [a, b, c, d, e, f, g, h, i] = matrix;
  const A = e * i - f * h;
  const B = f * g - d * i;
  const C = d * h - e * g;
  const determinant = a * A + b * B + c * C;
  if (!Number.isFinite(determinant) || Math.abs(determinant) < 1e-12) return null;
  return [
    A / determinant,
    (c * h - b * i) / determinant,
    (b * f - c * e) / determinant,
    B / determinant,
    (a * i - c * g) / determinant,
    (c * d - a * f) / determinant,
    C / determinant,
    (b * g - a * h) / determinant,
    (a * e - b * d) / determinant,
  ];
};

export const transformPoint = (matrix: PreviewTransform, x: number, y: number) => {
  const divisor = matrix[6] * x + matrix[7] * y + matrix[8];
  if (!Number.isFinite(divisor) || Math.abs(divisor) < 1e-12) return [x, y] as const;
  return [
    (matrix[0] * x + matrix[1] * y + matrix[2]) / divisor,
    (matrix[3] * x + matrix[4] * y + matrix[5]) / divisor,
  ] as const;
};

export const regionPolygon = (region: Region, previewToOriginal: PreviewTransform) => {
  const originalToPreview = invertTransform(previewToOriginal) ?? identityTransform;
  return [
    transformPoint(originalToPreview, region.left, region.top),
    transformPoint(originalToPreview, region.right, region.top),
    transformPoint(originalToPreview, region.right, region.bottom),
    transformPoint(originalToPreview, region.left, region.bottom),
  ]
    .map(([x, y]) => `${Math.max(0, Math.min(1, x)) * 100}% ${Math.max(0, Math.min(1, y)) * 100}%`)
    .join(", ");
};

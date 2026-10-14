/** 文字列が指定したユニオン型の要素のいずれかであるかを判定する。 */
export function isOneOf<T extends string>(value: string, candidates: readonly T[]): value is T {
  return candidates.some((candidate) => candidate === value);
}

/** 値がプロパティを持つオブジェクト(配列以外)であるかを判定する。 */
export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

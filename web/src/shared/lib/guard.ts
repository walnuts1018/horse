/** 文字列が指定したユニオン型の要素のいずれかであるかを判定する。 */
export function isOneOf<T extends string>(value: string, candidates: readonly T[]): value is T {
  return candidates.some((candidate) => candidate === value);
}

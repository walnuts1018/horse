/**
 * catch節などで受け取った不明な値をErrorへ正規化する。
 * ログや通知にはそのまま渡せるため、asによる型アサーションを使わずに例外値を扱える。
 */
export function toError(value: unknown): Error {
  if (value instanceof Error) {
    return value;
  }
  if (typeof value === "string") {
    return new Error(value);
  }
  if (
    value !== null &&
    typeof value === "object" &&
    "message" in value &&
    typeof value.message === "string"
  ) {
    return new Error(value.message);
  }
  return new Error(String(value));
}

/** 例外値から表示用のメッセージを取り出す。 */
export function errorMessage(value: unknown): string {
  return toError(value).message;
}

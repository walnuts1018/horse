# Gemini連携の未解決項目

## 利用規約の解釈

`docs/plan.md`ではGemini Developer APIのFree・Paid quotaを利用者が選択できる方針にしています。Free quotaに対する情報分類制限や自動マスキングはアプリ仕様に含めません。規約の適用判断は人が行いますが、判断待ちを理由に本番APIや現行のデータ送信動作を停止しません。Unpaid Servicesでは「Do not submit sensitive, confidential, or personal information to the Unpaid Services.」と定められています。Paid Servicesではpromptや応答を製品改善に使わず、違反検出などのため限られた期間ログに記録する条件を記載しました。

同規約の「for professional or business purposes, not for consumer use」は、本人のみの非公開利用であるHorseにconsumer useとして適用される可能性がある、という解釈を記載しました。規約本文には分類の定義がありません。18歳以上というAPI利用者の条件も記載し、Horseが本人のみの非公開利用で、未成年者向けではなく、年齢確認UIを設けないことを反映しました。規約の適用判断と現行API運用を分けており、Vertex AIなどへの移行は提案していません。

## Batch API

Gemini Batch APIのクライアント、job状態管理、結果取得経路は未実装です。大量のbackground jobで通常APIのquota予約がボトルネックになる場合に、Batch API対応を検討してください。

正解ラベルのない試行記録があります。この記録をモデル精度の根拠として扱いません。

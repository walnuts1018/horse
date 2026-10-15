# Gemini連携の未解決項目

## 利用規約の解釈

`docs/plan.md`ではGemini Developer APIのFree・Paid quotaを利用者が選択できる方針にしています。Free quotaに対する情報分類制限や自動マスキングはアプリ仕様に含めません。規約の適用判断は人が行いますが、判断待ちを理由に本番APIや現行のデータ送信動作を停止しません。Unpaid Servicesでは「Do not submit sensitive, confidential, or personal information to the Unpaid Services.」と定められています。Paid Servicesではpromptや応答を製品改善に使わず、違反検出などのため限られた期間ログに記録する条件を記載しました。

同規約の「for professional or business purposes, not for consumer use」は、本人のみの非公開利用であるHorseにconsumer useとして適用される可能性がある、という解釈を記載しました。規約本文には分類の定義がありません。18歳以上というAPI利用者の条件も記載し、Horseが本人のみの非公開利用で、未成年者向けではなく、年齢確認UIを設けないことを反映しました。規約の適用判断と現行API運用を分けており、Vertex AIなどへの移行は提案していません。

## Batch API

Gemini Batch APIのクライアント、job状態管理、結果取得経路は未実装です。大量のbackground jobで通常APIのquota予約がボトルネックになる場合に、Batch API対応を検討してください。

## source kindごとの精度評価

オフライン評価器はsource kindごとに、品目検出precision・recall、印字金額一致率、税額整合率、名称正規化一致率、証跡precisionを集計し、保存済みの抽出実行がある場合はモデル別の精度、推定費用、平均レイテンシも集計します。カード明細は取引行を品目評価へ変換し、加盟店名・金額・証跡領域を比較します。カード明細には税額整合率を適用せず、`taxConsistencyDocuments`を0にします。`mise run evaluation:run`で出力JSONの`sourceKinds`を確認できます。Gemini APIは呼び出しません。

同梱fixtureは架空のレシート、カード明細、注文メールを含み、税区分付き品目、値引、ポイント利用、明細行、証跡領域を評価器へ通します。fixtureにはモデル名・費用・遅延・モデル出力を記録していないため、出力されたsource kind別の精度は評価器の動作例であり、AIモデルの精度や実利用時の費用を示しません。実モデルを比較するには、同意を得た非公開評価データ上で各モデルの保存済み出力を評価データセットへ追加する必要があります。source kindごとの実測精度、モデル比較、費用、レイテンシは未評価です。既定モデルや自動振り分けを変更する根拠もありません。

正解ラベルのない試行記録があります。この記録をモデル精度の根拠として扱いません。

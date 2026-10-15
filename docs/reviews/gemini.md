# Gemini連携の未解決項目

## 1. Batch API

Gemini Batch APIのクライアント、job状態管理、結果取得経路は未実装です。大量のbackground jobで通常APIのquota予約がボトルネックになる場合に、Batch API対応を検討してください。

## 2. source kindごとの精度評価

オフライン評価器はケースの`sourceKind`ごとに、モデル別の品目検出precision・recall、印字金額一致率、税額整合率、商品名正規化一致率、証跡precision、推定費用、平均レイテンシを集計します。`mise run evaluation:run`で出力JSONの`sourceKinds`を確認できます。Gemini APIは呼び出しません。

同梱fixtureは人工的なレシート1件のみで、source kind間やモデル間を比較できるだけのデータがありません。税率混在、値引、ポイント、異なる入力source kindの合成fixtureと各モデルの保存済み実行結果を追加して評価する必要があります。現在のfixtureから既定モデルやsource kindごとの自動振り分けを変更する根拠は得られません。

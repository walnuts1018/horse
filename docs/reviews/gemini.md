# Gemini連携の未解決項目

## 1. Google側quota上限の自動取得

Worker間で共有するRPM・TPM・RPD予約、`CountTokens`による入力token予約、成功後の実績prompt token数への精算、`America/Los_Angeles`の暦日によるRPD集計、429時の共有cooldownは実装済みです。route/model別上限は`GEMINI_QUOTA_LIMITS`で設定し、未設定の組み合わせでは予約制御を行いません。無料Projectのquota超過時は有料Projectへ切り替えず、`Retry-After`または予約枠が空くまで再試行します。

コードはGoogle AI StudioやGemini APIから実際のRPM・TPM・RPD上限、quota回復時刻を取得しません。運用者が確認した値を`GEMINI_QUOTA_LIMITS`へ設定する必要があります。Google側から値を自動取得する公開APIが利用可能になった場合は、その取得方法とquota仕様を確認して追加を検討します。[Gemini APIのrate limits](https://ai.google.dev/gemini-api/docs/rate-limits)を参照してください。

## 2. Batch API

Gemini Batch APIのクライアント、job状態管理、結果取得経路は未実装です。大量のbackground jobで通常APIのquota予約がボトルネックになる場合に、Batch API対応を検討してください。

## 3. source kindごとの精度評価

オフライン評価器はケースの`sourceKind`ごとに、モデル別の品目検出precision・recall、印字金額一致率、税額整合率、商品名正規化一致率、証跡precision、推定費用、平均レイテンシを集計します。`mise run evaluation:run`で出力JSONの`sourceKinds`を確認できます。Gemini APIは呼び出しません。

同梱fixtureは人工的なレシート1件のみで、source kind間やモデル間を比較できるだけのデータがありません。税率混在、値引、ポイント、異なる入力source kindの合成fixtureと各モデルの保存済み実行結果を追加して評価する必要があります。現在のfixtureから既定モデルやsource kindごとの自動振り分けを変更する根拠は得られません。

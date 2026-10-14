# Gemini連携の未解決項目

## 1. Google側quota上限の自動取得

Worker間で共有するRPM・TPM・RPD予約、`CountTokens`による入力token予約、成功後の実績prompt token数への精算、`America/Los_Angeles`の暦日によるRPD集計、429時の共有cooldownは実装済みです。route/model別上限は`GEMINI_QUOTA_LIMITS`で設定し、未設定の組み合わせでは予約制御を行いません。無料Projectのquota超過時は有料Projectへ切り替えず、`Retry-After`または予約枠が空くまで再試行します。

コードはGoogle AI StudioやGemini APIから実際のRPM・TPM・RPD上限、quota回復時刻を取得しません。運用者が確認した値を`GEMINI_QUOTA_LIMITS`へ設定する必要があります。Google側から値を自動取得する公開APIが利用可能になった場合は、その取得方法とquota仕様を確認して追加を検討します。[Gemini APIのrate limits](https://ai.google.dev/gemini-api/docs/rate-limits)を参照してください。

## 2. Batch API

Gemini Batch APIのクライアント、job状態管理、結果取得経路は未実装です。大量のbackground jobで通常APIのquota予約がボトルネックになる場合に、Batch API対応を検討してください。

## 3. source kindごとの品質評価とモデル別料金表

モデル切替は現在メール系のLiteが429を返した場合に限られます。別モデルの無料枠を使う対象を広げる場合は、source kindごとの品質評価と、モデル別料金表の更新が必要です。レシートをより安価なモデルへ一律変更する前に、既存の[オフライン評価器](../../internal/evaluation)などの評価データを使い、税率混在、値引、ポイント、商品名の読取精度と推定費用を比較してください。

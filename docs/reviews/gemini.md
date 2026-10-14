# Gemini連携の未解決項目

## 1. 無料枠回復時刻の予測

Worker間で共有するRPM・TPM・RPD予約をPostgreSQLへ保存し、`CountTokens`の入力数を呼び出し前に予約します。成功時は実績のprompt token数へ更新します。`GEMINI_QUOTA_LIMITS`にroute/model別の上限がなければ予約制御は無効です。無料Projectのquota超過で有料Projectへ切り替えず、`Retry-After`または予約枠が空くまで待ちます。

RPD予約は`America/Los_Angeles`の暦日で集計し、日次上限到達後は次の太平洋時間午前0時まで再試行を待ちます。DST切替日も現地の暦日境界を使います。Google AI StudioはProject・modelごとの実際のRPM・TPM・RPD上限を表示しますが、このアプリからその上限値を取得する公開APIは確認できていません。`GEMINI_QUOTA_LIMITS`の設定値は運用者がAI Studioで手動確認し、変更時に更新する必要があります。上限値の動的取得とGoogle側の実際のquota回復時刻の取得は未対応です。[Gemini APIのrate limits](https://ai.google.dev/gemini-api/docs/rate-limits)を参照してください。

## 2. Flex tierとBatch APIの到達経路

通常のアップロードと手動再解析はAPIがjob purposeとして`interactive`を保存します。Routerは空または`interactive`をStandard tierとして扱います。`background`が与えられれば、無料Projectでは引き続きStandardを使い、有料Projectに進んだ場合にFlex tierを指定します。Pro昇格も元jobのpurposeを引き継ぎます。

現行APIには`background`を保存する経路がなく、通常jobからFlexを利用できません。RouterにはFlexに対応する分岐とFlex費用の推定がありますが、Batch APIのクライアント、job状態管理、結果取得経路はありません。メール一括取込や過去履歴の処理にFlexを使う案は、purposeをbackgroundに設定するjob経路の追加が必要です。Batchを選ぶ場合は、現在存在しない非同期Batch処理と結果回収も実装対象です。

## 3. source kindごとの品質評価とモデル別料金表

モデル切替は現在メール系のLiteが429を返した場合に限られます。別モデルの無料枠を使う対象を広げる場合は、source kindごとの品質評価と、モデル別料金表の更新が必要です。レシートをより安価なモデルへ一律変更する前に、既存の[オフライン評価器](../../internal/evaluation)などの評価データを使い、税率混在、値引、ポイント、商品名の読取精度と推定費用を比較してください。

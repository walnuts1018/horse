## 結論：無料枠を優先する経路はありますが、共有quota管理とジョブ分類はありません

現行コードでは、通常の抽出は無料Projectから開始します。無料Projectの429では有料Projectへ切り替えず、APIの`Retry-After`をWorkerのjob再試行へ渡します。Geminiが一時的な5xxを返した場合は、有料Projectへfallbackします。成功した呼び出し後に無料枠の使用量を集計したり、モデルごとのRPM・TPM・RPDを共有状態で管理したりする機能はありません。

### 現在の実装

| 項目 | 状態 |
| --- | --- |
| 無料Projectから抽出を開始 | 実装済み |
| レシート系とメール系で初回モデルを使い分け | 実装済み |
| メール系の無料Liteが429の場合に無料Flashを試す | 実装済み |
| 429後に`Retry-After`を使ってjobを再試行 | 実装済み |
| 無料Projectの一時的な5xx時に有料Projectへfallback | 実装済み |
| 有料ProjectのFlex tier | `background` purposeの場合に利用可能。ただし現行APIのjobはすべて`interactive`を設定 |
| Gemini Batch API | 未実装 |
| モデル別quotaの共有管理、無料枠の回復待ち | 未実装 |
| アプリ内の月間予算予約 | 未実装。Google Cloud Spend Capを使用 |

Geminiの有効化設定も確認が必要です。[README](../../README.md)と[デプロイ手順](../deployment.md)には`GEMINI_ENABLED=true`を設定するまで画像・メール解析jobを待機させると記載されています。デプロイ状態がこの設定に従う間、Gemini APIは通常の取り込みから呼び出されません。

## Routerのモデルとfallback

現行の初期モデルは[Router](../../internal/ingestion/router.go)に定数で定義され、`.env.example`から環境変数で上書きできます。

| 対象 | 初期モデル |
| --- | --- |
| レシート、画像、PDFなど | `gemini-3.8-flash` |
| `email`、`order_email`またはメールMIME | `gemini-3.5-flash-lite` |
| 税や合計の不整合等が残る場合のPro確認 | `gemini-3.1-pro-preview` |

Routerは初回モデルのquota fallbackを全source kindへ適用しません。メール系で初回の無料Liteが429を返した場合だけ、同じ無料ProjectのFlashを試します。無料Flashも429ならその応答の`Retry-After`をWorkerへ返し、無料Flashが一時的な5xxを返した場合は有料Flashへfallbackします。その他の無料429はWorkerのjob再試行へ進みます。レシート系のFlashからLiteへ切り替える処理や、quotaの残量を予測する処理はありません。

有料APIへのfallback対象はquotaではなく一時的なprovider errorです。無料APIの429とSpend Capエラーは有料APIへ切り替わりません。Spend Capエラーは通常の失敗回数を増やさずjobを再queueし、次の請求月境界を基準に再試行します。

モデル名を環境変数で変更する場合、Paidルート用の価格表にもモデルを登録する必要があります。例えば`GEMINI_LITE_MODEL=gemini-3.1-flash-lite`を設定しても、現行の`modelPrices()`には価格がないため、有料fallbackは`unknown_model_price`で拒否されます。価格表に未登録のモデルを無料ルートだけで使う場合は動作し得ますが、Paidルートへ進めません。

## FlexとBatchの到達経路

通常のアップロードと手動再解析はAPIがjob purposeとして`interactive`を保存します。Routerは空または`interactive`をStandard tierとして扱います。`background`が与えられれば、無料Projectでは引き続きStandardを使い、有料Projectに進んだ場合にFlex tierを指定します。Pro昇格も元jobのpurposeを引き継ぎます。

現行APIには`background`を保存する経路がなく、通常jobからFlexを利用できません。RouterにはFlexに対応する分岐とFlex費用の推定がありますが、Batch APIのクライアント、job状態管理、結果取得経路はありません。メール一括取込や過去履歴の処理にFlexを使う案は、purposeをbackgroundに設定するjob経路の追加が必要です。Batch利用は別途API実装が必要です。

## 費用制御

Gemini応答にUsageMetadataが含まれる場合、成功・失敗を問わず入力・出力token数と推定費用を記録します。Routerに月間予算の予約や呼び出し前の上限判定はありません。Google CloudのSpend Capを月500円で設定する運用は[デプロイ手順](../deployment.md)に記録されています。Spend CapはGoogle側で適用される制限で、アプリ内でAPI呼び出し前に支出を止める仕組みではありません。Spend Cap到達を示す応答を受けたjobは請求月境界を基準に再試行します。

Google CloudのSpend Capとは別にアプリ内予算を追加する必要性は現時点では低く、請求データと記録済み推定費用の照合精度を改善する際に再評価します。

## 改善候補

共有quota状態を持たないため、Worker間でモデル別RPM・TPM・RPDを調整できず、無料枠の回復時刻も予測しません。AI Studioで確認した実際の上限を用いる共有レート制御や、急がないjobの再試行時刻を設けると、quotaを超える呼び出しと不要な失敗を減らせます。

モデル切替は現在メール系のLiteが429を返した場合に限られます。別モデルの無料枠を使う対象を広げる場合は、source kindごとの品質評価と、モデル別料金表の更新が必要です。レシートをより安価なモデルへ一律変更する前に、オフライン評価データを使い、税率混在、値引、ポイント、商品名の読取精度と推定費用を比較してください。

大量の非対話処理へFlexを適用するには、jobのpurposeと処理期限を決める経路を用意します。Batchを選ぶ場合は、現在存在しない非同期Batch処理と結果回収も実装対象です。モデルの品質・費用比較には既存の[オフライン評価器](../../internal/evaluation)を利用できます。

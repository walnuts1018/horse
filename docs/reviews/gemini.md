# Gemini連携の未解決項目

## 1. RPM・TPM・RPDの予約制御と回復予測

Worker間で共有する429 cooldownをDBへ記録し、`Retry-After`または既存jobの再試行バックオフが切れるまで同じproject routeとmodelへの呼び出しを待つようにしました。quota上限前にRPM・TPM・RPDを調整する予約制御と、無料枠の回復時刻予測はありません。

実際のモデル別上限値を確認できた場合は、その値と`Retry-After`実測に基づく分散制御を検討してください。無料Projectの429で有料Projectへ切り替えない既存方針は維持します。

## 2. Flex tierとBatch APIの到達経路

通常のアップロードと手動再解析はAPIがjob purposeとして`interactive`を保存します。Routerは空または`interactive`をStandard tierとして扱います。`background`が与えられれば、無料Projectでは引き続きStandardを使い、有料Projectに進んだ場合にFlex tierを指定します。Pro昇格も元jobのpurposeを引き継ぎます。

現行APIには`background`を保存する経路がなく、通常jobからFlexを利用できません。RouterにはFlexに対応する分岐とFlex費用の推定がありますが、Batch APIのクライアント、job状態管理、結果取得経路はありません。メール一括取込や過去履歴の処理にFlexを使う案は、purposeをbackgroundに設定するjob経路の追加が必要です。Batchを選ぶ場合は、現在存在しない非同期Batch処理と結果回収も実装対象です。

## 3. source kindごとの品質評価とモデル別料金表

モデル切替は現在メール系のLiteが429を返した場合に限られます。別モデルの無料枠を使う対象を広げる場合は、source kindごとの品質評価と、モデル別料金表の更新が必要です。レシートをより安価なモデルへ一律変更する前に、既存の[オフライン評価器](../../internal/evaluation)などの評価データを使い、税率混在、値引、ポイント、商品名の読取精度と推定費用を比較してください。

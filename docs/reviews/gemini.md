# Gemini連携の未解決項目

## 1. 共有quota管理とレート制御

共有quota状態を持たないため、Worker間でモデル別RPM・TPM・RPDを調整できず、無料枠の回復時刻も予測しません。AI Studioで確認した実際の上限を用いる共有レート制御や、急がないjobの再試行時刻を設けると、quotaを超える呼び出しと不要な失敗を減らせます。

現行コードでは、通常の抽出は無料Projectから開始し、無料Projectの429では有料Projectへ切り替えず、APIの`Retry-After`をWorkerのjob再試行へ渡します。成功した呼び出し後に無料枠の使用量を集計したり、モデルごとのRPM・TPM・RPDを共有状態で管理したりする機能はありません。

## 2. Flex tierとBatch APIの到達経路

通常のアップロードと手動再解析はAPIがjob purposeとして`interactive`を保存します。Routerは空または`interactive`をStandard tierとして扱います。`background`が与えられれば、無料Projectでは引き続きStandardを使い、有料Projectに進んだ場合にFlex tierを指定します。Pro昇格も元jobのpurposeを引き継ぎます。

現行APIには`background`を保存する経路がなく、通常jobからFlexを利用できません。RouterにはFlexに対応する分岐とFlex費用の推定がありますが、Batch APIのクライアント、job状態管理、結果取得経路はありません。メール一括取込や過去履歴の処理にFlexを使う案は、purposeをbackgroundに設定するjob経路の追加が必要です。Batchを選ぶ場合は、現在存在しない非同期Batch処理と結果回収も実装対象です。

## 3. source kindごとの品質評価とモデル別料金表

モデル切替は現在メール系のLiteが429を返した場合に限られます。別モデルの無料枠を使う対象を広げる場合は、source kindごとの品質評価と、モデル別料金表の更新が必要です。レシートをより安価なモデルへ一律変更する前に、既存の[オフライン評価器](../../internal/evaluation)などの評価データを使い、税率混在、値引、ポイント、商品名の読取精度と推定費用を比較してください。

# Horse バックエンドの保守性・ライブラリ活用レビュー

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse) 2026年10月14日の`main`作業ツリー

## 総評

GraphQL SDLの機能別分割、BobによるDBアクセス、購入・入金・送金・精算のApplication層への移行、PostgreSQLのOpenTelemetry計測が実装されています。購入更新時の会計同期と複数月精算の一括読込も実装済みです。

現行コードで残っている主な課題は、口座間移動・外部入金・予算など一部Mutationの業務処理がGraphQL層に残ることと、Gemini SchemaをGo型と別々に管理することです。口座残高分析、購入訂正、品目更新は専用QueryまたはApplication Serviceへ移行済みです。残件は既存のBob・pgx・gqlgenを使った責務分離で対応できます。

| 領域 | 現在の選択 | 判断 |
| --- | --- | --- |
| API | Go + gqlgen | 維持 |
| DBアクセス | Bob + pgx | 維持・DSLを活用 |
| DBマイグレーション | Atlas | 維持 |
| 認証 | go-oidc + x/oauth2 | 維持 |
| S3 | AWS SDK for Go v2 | 維持 |
| メッセージング | amqp091-go | 維持 |
| 監視 | OpenTelemetry | 維持 |
| Gemini | Go公式SDK | 維持 |

## 1. Mutationの責務分離

購入作成・確定・更新・訂正、品目更新、入金、送金、精算にはApplication Serviceがあります。一方、口座間移動と外部入金は[account_movements.go](../../internal/graph/account_movements.go)、予算は[budget.go](../../internal/graph/budget.go)、定期支出は[recurring_expense.go](../../internal/graph/recurring_expense.go)にトランザクションと永続化処理が残っています。

GraphQL層には認可、入力検証、Application層への変換、結果のGraphQL型変換を置き、ユースケースのトランザクションと会計処理は既存のApplicationパッケージへ移すのが妥当です。移行時は既存の会計不変条件と冪等性を維持し、GraphQL生成型をApplication層へ持ち込まないでください。

## 2. Gemini SchemaとGo型

Gemini通信には`google.golang.org/genai`を使用しています。[gemini.go](../../internal/ingestion/gemini.go)の`receiptSchema()`は`map[string]any`で定義され、[gemini_test.go](../../internal/ingestion/gemini_test.go)の`TestReceiptSchemaMatchesJSONTypesAndRequiredFields`がReceipt型とのプロパティ・型・必須項目の一致を検査します。

二重管理は残りますが、現状の契約検査で主要なずれを検出できます。Schema自動生成へ移行する場合はGemini固有制約も検査できることを確認してから判断してください。

## 3. DBトランザクション

GraphQLとApplicationの一部には`Begin`、`Commit`、`Rollback`の手動管理が残っています。購入・入金・送金・精算のApplication Serviceも`context.WithoutCancel(ctx)`を使ってRollbackします。

手動管理は現状で誤りとは言えず、`pgx.BeginFunc`への一括置換は不要です。トランザクション境界をResolverに持つユースケースをApplicationへ移し、Rollback時のキャンセル後処理とタイムアウト挙動を維持してください。

## 4. 読み取り処理の配置

月次支出、商品候補、口座残高分析は`internal/postgres/query`へ分離済みです。口座残高分析は専用Query内で対象口座の仕訳を全件読み、期間開始残高と各行残高を計算します。全期間の残高を正確に返す必要があるため、履歴ページのページサイズだけではDB読込量を減らせません。

GraphQL層からの分離は完了しています。DB読込量の削減は未実施で、性能計測もありません。改善する場合は全期間残高と履歴順序を維持できる集約方法を計測してから判断してください。

## 5. DBトレーシングとテストデータ

[internal/postgres/pool.go](../../internal/postgres/pool.go)は`otelpgx.NewTracer()`をpgxの接続設定へ登録し、APIとWorkerが共通Pool生成関数を使います。DBトレーシングの未導入という指摘は解決済みです。

[bobgen.yaml](../../bobgen.yaml)ではBob Factoryが無効です。統合テストはDB条件を明示しているため、Factory導入は必須ではありません。セットアップ重複の削減が必要になった箇所で、会計上重要な値を暗黙化しない小さなFixtureを検討してください。

## 6. DBアクセスとイベント通知

複雑な読み取りもBob DSLで表現します。生SQLを文字列で管理する方式やBob Query Code Generationへの移行は採用しません。

SSEと購入Subscriptionは現在も定期ポーリングを使います。接続数とDB負荷の実測がないため、通知方式の変更は現時点の対応項目にしません。

## ライブラリ採用の最終判断

| ライブラリ・機能 | 判断 |
| --- | --- |
| Bob ORM・Query Builder・リレーションローダー | 維持・DSLを活用 |
| `google.golang.org/genai` | 維持 |
| `otelpgx` | 採用済み |
| Bob Factory | 必要なFixture重複が確認された場合に検討 |
| gqlgen DataLoader | Node参照の独自バッチ処理で足りない経路があれば個別に検討 |
| GORM・Entなど別ORM | 不要 |
| 別のHTTPフレームワーク | 不要 |
| DIコンテナ | kessokuを使用 |

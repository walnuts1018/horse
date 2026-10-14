# バックエンド構成の未解決事項

対象は2026年10月14日時点の作業ツリーです。

## Gemini SchemaとGo型

Geminiの構造化出力Schemaは[gemini.go](../../internal/ingestion/gemini.go)の`receiptSchema()`で定義され、GoのReceipt型とは別に管理されています。[gemini_test.go](../../internal/ingestion/gemini_test.go)の`TestReceiptSchemaMatchesJSONTypesAndRequiredFields`が型、必須項目、JSON型の一致を検査します。二重管理は残るため、Schema変更時はGemini固有制約も検査できる自動生成方法が見つかった場合に再評価してください。

## 口座残高Queryの読込量

口座残高分析は[account_balance_analysis.go](../../internal/postgres/query/account_balance_analysis.go)でQuery層へ分離済みです。期間開始残高と各行残高を返すため対象口座の全仕訳を読み込みます。ページ表示のための履歴上限では読込量を減らせず、集約方式の性能計測もありません。履歴順序と全期間残高を維持できる方法を計測してから改善を判断してください。

## Application Serviceのtransaction管理

Application Serviceは`Begin`と`Commit`を明示し、deferから`context.WithoutCancel(ctx)`を使ってRollbackします。defer内のRollback失敗は`internal/application/txcleanup`で記録します。一部の統合テストは失敗時の更新取り消しを確認しますが、全Serviceの失敗経路は網羅していません。現時点で共通transaction抽象や`pgx.BeginFunc`への移行が必要だと示す不具合は確認されていないため、失敗時のテストが必要になった場合に再評価します。

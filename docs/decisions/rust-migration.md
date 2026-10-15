# Rust移行

[Domain Modeling Made Functional](../dmmf/summary.md)も参考にする。

## 対象範囲

Frontendは現行のTypeScript実装を維持します。BackendのGo実装はGraphQL API、REST API、Worker、mailbox ingestion、batch、評価CLIを含めてRustへ移行します。GraphQL Schemaを公開契約として維持し、APIとWorkerは別の実行バイナリにします。Domain Modeling Made Functionalの考え方に沿い、Bounded ContextごとにPure Domain、Workflow、Infrastructure Adapterを分離します。

## 本番DB

Rust版への切り替え時に本番DBをリセットし、新しいDB Schemaで再作成します。この操作はRust版のAPI、Worker、GraphQL契約、デプロイ定義の検証が完了し、切り替えを実施する段階で行います。切り替え前に稼働中のGo環境のDBを変更しません。

## PostgreSQLアクセス

DBアクセスはDiesel 2.3と`diesel-async` 0.9.2を使用し、型付きQuery DSLで実装します。Atlasをスキーマとmigrationの正本として継続し、Atlas適用済みのDBから`diesel print-schema`で型定義を生成します。Domain型とDieselの永続化型はAdapterで変換します。アプリケーションコードでは生SQLを発行する`sql_query`を使いません。

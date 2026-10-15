# Rust移行

## 状態

進行中。Rust WorkspaceをBounded ContextごとのDomain・Application・PostgreSQL Adapterへ再編し、GraphQL APIとWorkerをComposition Rootから組み立てる。Rust APIとWorkerへ全機能を移し、契約・業務動作・運用確認を完了した後にGo APIとWorkerを一括で置き換える。

## 決定

BackendをGoからRustへ移行する。GraphQL Schemaと既存の公開契約を維持し、クライアントに移行を要求しない。Rust API、Worker、GraphQL契約、デプロイ定義の検証が完了した後、Go実装をRust実装へ一括で置き換える。切り替え段階で本番DBをリセットし、新しいschemaを作成する。

PostgreSQLのスキーマとmigrationの正本にはAtlasを使う。RustからのDBアクセスにはDiesel 2.3と`diesel-async` 0.9.2を使い、Atlas適用後のスキーマから生成した型定義を基に、型付きQuery DSLで実装する。Dieselのmigration機能と`sql_query`による生SQLは使わない。DSLで表現できない要件はクエリやスキーマを見直して解決する。

業務処理はDMMFに沿って、入力と現在状態から結果・イベント・失敗を値として返すpure Decisionと、DBや外部サービスのI/Oを行う実行境界に分ける。DecisionはI/O、GraphQL、永続化型に依存させない。実行境界が必要な読み込みを行い、Decisionを呼び出し、その結果に基づいて書き込みとイベント配送を行う。

Workspaceは`apps/`にComposition Root、`contexts/<bounded-context>/{domain,application,postgres}`に業務ごとの実装、`adapters/`にGraphQL・HTTP・Identity・AI・Workerの接続、`platform/postgres-common`に接続Poolと生成済みDiesel Schemaを配置する。複数Contextを横断する読み取りと更新は`contexts/cross-context/postgres`に配置し、単一Contextのデータ所有権へ混在させない。

Application Workflowは業務操作のLoad・Decision・Persistの順序を所有し、小さな操作契約を通じてPostgreSQL AdapterへI/Oを要求する。PostgreSQL AdapterがDieselのtransactionとロックを維持し、Workflow全体を原子的に実行する。GraphQL ResolverはDTOを認証済みActor付きCommandへ変換し、Application Workflowを呼び出す。

## 移行順序

Rust APIで既存のGraphQL契約とHTTP契約を実装し、Go APIの業務動作と一致することを検証する。Rust Workerへ非同期処理を移し、ジョブ状態、再試行、冪等性、停止後の回復を既存の外部契約と整合させる。全機能の検証後にGo APIとWorkerをRust実装へ一括で置き換える。

## Go実装の終了条件

GoのBackend実装を削除できるのは、次の条件をすべて満たした後とする。

- GraphQL Schemaと公開APIの入出力・エラー・認可の契約が維持されている。
- APIとWorkerの全業務機能、定期処理、外部サービス連携がRustで実行できる。
- 一括切り替え時の本番DBリセットと新schema作成の手順を検証している。
- 再試行、重複実行、同時更新、停止後の回復を含め、運用上必要な処理保証がRust側で成立している。
- デプロイ、監視、障害対応の手順がRustのAPIとWorkerを対象としている。

## 理由

GraphQLとHTTPの公開契約を維持し、既存クライアントへの変更を避ける。Rustで全機能の置き換えと業務動作を検証してからGo実装を一括で置き換える。本番DBのリセットはRust API、Worker、契約、デプロイ定義を検証した後に行う。

Atlasを正本にしてDB変更の適用履歴とスキーマを一元管理する。Dieselの型付きDSLはDBアクセスの型検査を提供し、pure DecisionとI/Oの分離は業務判断を外部状態から独立して表現する。

詳細な設計背景と検討事項は[rust.md](./rust.md)を参照する。

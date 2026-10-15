# Rust移行

## 状態

進行中。Rust workspaceではBookkeepingのDomainとWorkflow、PostgreSQL Adapterの実装が始まっている。GoのAPIとWorkerは稼働を維持し、業務機能をRustへ段階的に移行する。

## 決定

BackendをGoからRustへ移行する。移行中もGraphQL Schemaと既存の公開契約を維持し、クライアントに移行を要求しない。Rust API、Worker、GraphQL契約、デプロイ定義の検証が完了した後、切り替え段階で本番DBをリセットし、新しいschemaを作成する。切り替え前は稼働中のGo環境のDBを変更しない。

PostgreSQLのスキーマとmigrationの正本にはAtlasを使う。RustからのDBアクセスにはDiesel 2.3と`diesel-async` 0.9.2を使い、Atlas適用後のスキーマから生成した型定義を基に、型付きQuery DSLで実装する。Dieselのmigration機能と`sql_query`による生SQLは使わない。DSLで表現できない要件はクエリやスキーマを見直して解決する。

業務処理はDMMFに沿って、入力と現在状態から結果・イベント・失敗を値として返すpure Decisionと、DBや外部サービスのI/Oを行う実行境界に分ける。DecisionはI/O、GraphQL、永続化型に依存させない。実行境界が必要な読み込みを行い、Decisionを呼び出し、その結果に基づいて書き込みとイベント配送を行う。

## 移行順序

まずRust APIをGraphQL契約に接続し、機能単位でGo APIの処理を置き換える。Rust実装の機能が契約と業務上の振る舞いを満たしたことを確認してから、その機能のGo実装を削除する。続いてRust Workerへ非同期処理を移し、ジョブ状態、再試行、冪等性、停止後の回復を既存の外部契約と整合させる。併存環境でGoとRustの各プロセスが同じDBを扱う場合は、両実装でスキーマと状態遷移の互換性を保つ。

## Go実装の終了条件

GoのBackend実装を削除できるのは、次の条件をすべて満たした後とする。

- GraphQL Schemaと公開APIの入出力・エラー・認可の契約が維持されている。
- APIとWorkerの全業務機能、定期処理、外部サービス連携がRustで実行できる。
- Go/Rust併存環境で同じDBを扱う場合は、読み書き互換性を確認している。切り替え時の本番DBリセットと新schema作成の手順も検証している。
- 再試行、重複実行、同時更新、停止後の回復を含め、運用上必要な処理保証がRust側で成立している。
- デプロイ、監視、障害対応の手順がRustのAPIとWorkerを対象としている。

## 理由

GraphQL契約を維持した段階移行なら、既存クライアントへの変更を避けながら機能ごとにRust実装を検証できる。Go実装を先に削除すると、未移行機能や既存の業務上の工夫を失うおそれがあるため、Rustでの置き換えを確認してから削除する。本番DBのリセットはRust API、Worker、契約、デプロイ定義を検証した後に行い、切り替えまで稼働中のGo環境に影響を与えない。

Atlasを正本にすることで、GoとRustの併存中もDB変更の適用履歴とスキーマを一元管理できる。Dieselの型付きDSLはDBアクセスの型検査を提供し、pure DecisionとI/Oの分離は業務判断を外部状態から独立して表現する。

詳細な設計背景と検討事項は[rust.md](./rust.md)を参照する。

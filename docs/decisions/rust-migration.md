# Rust移行

## 状態

Rust移行は本番へ反映済み。CLIを除くGoの実行コードをRustへ移し、Go Backendを削除した。空のPostgreSQLへAtlas migrationを適用してDiesel schemaを生成し、本番DBを再作成した。2026年10月15日時点でRust APIとWorkerはKubernetes上でReadyになり、`horse.walnuts.dev`と`/healthz`はHTTP 200を返した。

## 決定

BackendをGoからRustへ移行する。GraphQL Schemaと既存の公開契約を維持し、クライアントに移行を要求しない。Rust API、Worker、GraphQL契約、デプロイ定義の検証が完了した後、Go実装をRust実装へ一括で置き換える。切り替え段階で本番DBをリセットし、新しいschemaを作成する。

PostgreSQLのスキーマとmigrationの正本にはAtlasを使う。RustからのDBアクセスにはDiesel 2.3と`diesel-async` 0.9.2を使い、Atlas適用後のスキーマから生成した型定義を基に、型付きQuery DSLで実装する。Dieselのmigration機能と`sql_query`による生SQLは使わない。DSLで表現できない要件はクエリやスキーマを見直して解決する。

業務処理はDMMFに沿って、入力と現在状態から結果・イベント・失敗を値として返すpure Decisionと、DBや外部サービスのI/Oを行う実行境界に分ける。DecisionはI/O、GraphQL、永続化型に依存させない。実行境界が必要な読み込みを行い、Decisionを呼び出し、その結果に基づいて書き込みとイベント配送を行う。

Workspaceは`apps/`にComposition Root、`contexts/<bounded-context>/{domain,application,postgres}`に業務ごとの実装、`adapters/`にGraphQL・HTTP・Identity・AI・Workerの接続、`platform/postgres-common`に接続Poolと生成済みDiesel Schemaを配置する。複数Contextを横断する読み取りと更新は`contexts/cross-context/postgres`に配置し、単一Contextのデータ所有権へ混在させない。

Application Workflowは業務操作のLoad・Decision・Persistの順序を所有し、小さな操作契約を通じてPostgreSQL AdapterへI/Oを要求する。PostgreSQL AdapterがDieselのtransactionとロックを維持し、Workflow全体を原子的に実行する。GraphQL ResolverはDTOを認証済みActor付きCommandへ変換し、Application Workflowを呼び出す。

GraphQL境界ではApplication Decisionの業務エラーを入力・状態エラーへ変換し、PostgreSQLのI/O障害と永続化データの破損は内部エラーとして扱う。PostgreSQL Adapter内でも呼び出し側Decisionの不整合と保存済みデータの破損を別のエラーとして表現する。

購入訂正の`journals.occurred_at`とLedger Entryの日時には訂正を記録した時刻を保存する。`effective_period`は訂正対象として指定したOpen会計期間と月次精算を決める値であり、記録日時を過去へ変更しない。

## 残作業

- すべての更新Workflowで共通の操作権限契約を検証する。
- Context横断Workflowをcross-context Adapterへ整理し、各Contextのテーブル所有権を維持する。
- 作業ツリーの変更をまとめて検証し、必要なHorseとinfraの更新をデプロイして稼働状態を確認する。

## 理由

GraphQLとHTTPの公開契約を維持し、既存クライアントへの変更を避ける。Rustで全機能の置き換えと業務動作を検証した後にGo実装を一括で置き換え、本番DBを承認済みの新schemaで再作成した。

Atlasを正本にしてDB変更の適用履歴とスキーマを一元管理する。Dieselの型付きDSLはDBアクセスの型検査を提供し、pure DecisionとI/Oの分離は業務判断を外部状態から独立して表現する。

詳細な設計背景と検討事項は[rust.md](./rust.md)を参照する。

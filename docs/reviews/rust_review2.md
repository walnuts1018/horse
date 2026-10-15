# Rust実装の残存レビュー項目

確認日: 2026年10月16日

このファイルには、現在もコード上で確認できる検討事項だけを記載します。解決済みの指摘は削除しました。

## 1. WorkspaceとBounded Context

Rust Workspaceは`apps/`、`contexts/<bounded-context>/{domain,application,postgres}`、`contexts/cross-context/postgres`、`adapters/`、`platform/postgres-common`に分割されています。Context単位のcrate配置とGraphQL・WorkerのComposition Rootは、現在のディレクトリ構成と一致しています。

Policy ContextはBudget、RecurringExpense、AllocationRule、入金予定（`funding_schedules`）を所有します。月次のfunding obligationsとsettlementsはBookkeeping Contextが所有します。

## 2. WorkflowとTransaction

購入作成、購入作成と即時投稿、単独投稿、購入訂正、月次締め、資金記録はApplication WorkflowがLoad・Decision・Persistを順序付けます。PostgreSQL AdapterはWorkflow実行中に一つのDiesel transactionと`AsyncPgConnection`を保持します。資金記録の冪等性確認、事実の読み込み、決定、保存も同じtransactionとconnectionで行います。

月次締め、投稿、訂正、資金記録は会計期間行を先にロックします。訂正が複数期間を参照する場合は期間開始日順でロックし、購入行を後からロックします。Purchaseの状態は`DRAFT`と`POSTED`だけです。月次締めは購入状態や購入revisionを変更せず、精算結果と締め時点の購入revisionを保存します。GraphQLで互換性のため`CLOSED`を返す場合はAccountingPeriodから導出します。

ApplicationのDecision errorとPostgreSQL AdapterのI/O errorはGraphQL境界で分けて扱います。購入作成・投稿の業務失敗はUserErrorへ変換され、接続障害や永続化データの不整合は内部エラーとして扱われます。PostgreSQL Adapter内では`InvalidDecision`が保存前の整合性検査失敗にも使われるため、保存済みデータの破損と呼び出し側Decisionの不一致を明示的に区別する余地があります。

## 3. イベントの配送

`household_events`はGraphQL変更通知の保存に使われています。Context間イベントを確実に配送するTransactional Outboxは実装されていません。Context間の非同期連携が必要になった時点で、公開イベントの契約、重複配送時の冪等性、再試行と保管期間を定義してください。

## 4. 数量単位

`Measure`は数量の分子・分母と単位を検証しますが、`1 kg`と`1000 g`の換算や同値判定は行いません。商品比較や数量集計で単位換算が必要になった場合は、対応単位と丸め規則を業務仕様に定めてから実装してください。

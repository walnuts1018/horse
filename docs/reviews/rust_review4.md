# Horse Rust実装の設計上の未解決事項

## 前提

2026年10月16日時点の作業ツリーを確認しました。以下は改善案であり、現行コードが不正または故障しているという主張ではありません。業務要件に照らして、採用する範囲を決める必要があります。

## Workflowの集約

Workflow構成は複数の層にあります。[`close_period.rs`](../../contexts/bookkeeping/postgres/src/close_period.rs)ではPostgreSQL実装がロード、判断、永続化を含む操作全体を担っています。Application Workflowから順序を読める形に揃えることは、引き続き検討事項です。

## 仕訳モデル

[`journal.rs`](../../contexts/bookkeeping/domain/src/journal.rs)の`BalancedJournal`と、各Workflowの明細モデルが併存しています。全Workflowで共有する会計不変条件と、操作ごとに残すべき差異を整理する余地があります。

## Bounded Contextとテーブル所有権

[`purchase_command_context.rs`](../../contexts/bookkeeping/postgres/src/purchase_command_context.rs)などでは、Bookkeepingの処理から他Contextが所有するテーブルを参照します。共有DB自体ではなく、テーブルの更新規則と所有者をどのContextに置くかが検討事項です。現在の整合性境界を保つための直接参照か、Context間の契約へ移すかを業務モデルに基づいて決める必要があります。

## Aggregateの復元

[`post_purchase_loader.rs`](../../contexts/bookkeeping/postgres/src/post_purchase_loader.rs)はDB値から`DraftPurchase`を直接組み立てます。個々のValue Objectの検証に加え、Aggregate全体の復元条件をDomainの構築経路で表現する案があります。

## 検証済み入力の表現

[`set_allocation_rule.rs`](../../contexts/policy/application/src/set_allocation_rule.rs)の`ValidatedSetAllocationRuleCommand`は公開フィールドを持ちます。検証後の値を外部から直接構築できない形にし、検証結果を型で保持する余地があります。

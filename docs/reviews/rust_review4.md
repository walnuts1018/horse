# Horse Rust実装の設計上の未解決事項

## 前提

2026年10月16日時点の作業ツリーを確認しました。以下は改善案であり、現行コードが不正または故障しているという主張ではありません。業務要件に照らして、採用する範囲を決める必要があります。

## 仕訳モデル

[`journal.rs`](../../contexts/bookkeeping/domain/src/journal.rs)の`BalancedJournal`と、各Workflowの明細モデルが併存しています。全Workflowで共有する会計不変条件と、操作ごとに残すべき差異を整理する余地があります。

## Bounded Contextとテーブル所有権

[`purchase_command_context.rs`](../../contexts/bookkeeping/postgres/src/purchase_command_context.rs)などでは、Bookkeepingの処理から他Contextが所有するテーブルを参照します。共有DB自体ではなく、テーブルの更新規則と所有者をどのContextに置くかが検討事項です。現在の整合性境界を保つための直接参照か、Context間の契約へ移すかを業務モデルに基づいて決める必要があります。

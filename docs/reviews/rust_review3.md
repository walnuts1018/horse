# Horse Rust実装の設計レビュー

## 前提

2026年10月16日時点の作業ツリーを確認した設計レビューです。対象はRust workspaceのDomain、Application、PostgreSQL、GraphQL、Workerです。以下はコードが誤動作しているという指摘ではなく、型による制約や責務の置き場所を改善するための未解決の提案です。業務結果の正しさや実DBでの動作は検証していません。

## 継続して検討したい項目

### 仕訳モデルの共有範囲

[`journal.rs`](../../contexts/bookkeeping/domain/src/journal.rs)の`BalancedJournal`は貸借一致を保証しますが、各Workflowは個別の仕訳明細型も保持しています。共通の会計不変条件とWorkflow固有の表現をどの範囲で共有するかは未決です。

### Event永続化とWorkflowの境界

複数のPostgreSQL実装が`household_events`向けのイベント項目を組み立てています。イベント内容を誰が決定し、各配送形式へ誰が変換するかを整理する余地があります。

## 評価

DomainにはSmart Constructorや`BalancedJournal`など、不変条件を型に寄せた設計があります。今後の設計判断では、既存の強みを維持しつつ、上記の未解決項目を業務上の境界に合わせて段階的に扱うことを提案します。

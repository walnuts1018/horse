# Rustレビューの未解決項目

## Domain型

Policy、Catalog、EvidenceのDomain CommandとDecisionには識別子や制約値を`String`や公開フィールドで保持する型が残っている。識別子の取り違えや不正値を防ぐ型、検証済み状態を表す型を各Bounded Contextの業務上の不変条件に合わせて整備する。

## Bounded Contextのデータ所有権

Pool、Allocation Rule、Funding Rule、Household Defaultsの業務ルールと状態変更を所有するBounded Contextが未確定である。`contexts/cross-context/postgres/src/pool_mutations.rs`の更新処理を移す先は、所有権の決定後に整理する。

## GraphQLとPostgreSQLの依存境界

`adapters/graphql`は複数の`*-postgres` crate、`diesel`、`diesel-async`、`PgPool`に直接依存している。Catalogのタグ一覧QueryはApplicationの`TagQuery`契約を経由する構成に移行した。商品Queryと他ContextのQuery・MutationはPostgreSQL実装へ直接依存するため、Applicationの契約とWorkflowへの移行が残っている。

## Pool設定MutationのError境界

Pool設定のGraphQL MutationはPostgreSQL Persistence Errorを直接分類している。Poolの所有Contextを決定した後にApplication Workflowで失敗を分類し、GraphQLでは業務上の失敗と内部障害だけを扱う。

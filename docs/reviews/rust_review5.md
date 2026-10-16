# Rustレビューの未解決項目

## GraphQLとPostgreSQLの依存境界

`adapters/graphql`は複数の`*-postgres` crate、`diesel`、`diesel-async`、`PgPool`に直接依存している。CatalogのTag・Product・Node batch QueryとCategoryを含むCatalog Mutation、Account Balance QueryはApplication契約を経由する。Pool・Funding・AllocationのMutationと他ContextのQuery・MutationにはPostgreSQL実装への直接依存が残っているため、Applicationの契約とWorkflowへの移行が必要である。

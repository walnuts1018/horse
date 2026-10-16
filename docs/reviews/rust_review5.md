# Rustレビューの未解決項目

## GraphQLとPostgreSQLの依存境界

`adapters/graphql`は複数の`*-postgres` crate、`diesel`、`diesel-async`、`PgPool`に直接依存している。CatalogのTag・Product・Node batch QueryとCatalog Mutation、BookkeepingのAccount Balance・Account History・月次支出・購入既定値QueryはApplication契約を経由する。Pool funding ruleの判断と失敗分類はPolicy Application Workflowへ移したが、GraphQLからPostgres adapterを直接呼び出している。Account Balance Analysis、Household、Funding、Allocation、Purchase、EvidenceなどのQuery・MutationにもPostgres実装への直接依存が残っており、Application契約とPortへの移行が必要である。

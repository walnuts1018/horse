# Rustレビューの未解決項目

## GraphQLとPostgreSQLの依存境界

`adapters/graphql`は複数の`*-postgres` crate、`diesel`、`diesel-async`、`PgPool`に直接依存している。Account balance・balance analysis・history・monthly spending・purchase defaultsとLedgerEntry・Purchase・Observation Node batch、Household Query、Allocation Rule・Funding Schedule Query、SetAllocationRule・SetFundingSchedule mutationはApplication Portを経由する。未解決の境界は、GraphQL内のDiesel Query (`node_financial_batch.rs`、`node_batch_catalog_planning.rs`、`subscriptions.rs`)、Pool funding rule・Pool creation・default allocation、Household lifecycle・membership、Purchase・Statement・EvidenceのCommandとRead modelである。これらをApplication契約とPortへ移し、GraphQLから`*-postgres`・Diesel・PgPoolへの直接依存を除く必要がある。

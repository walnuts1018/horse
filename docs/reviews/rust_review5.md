# Rustレビューの未解決項目

## GraphQLとPostgreSQLの依存境界

`adapters/graphql`は複数の`*-postgres` crate、`diesel`、`diesel-async`、`PgPool`に直接依存している。Account balance・balance analysis・history・monthly spending・purchase defaultsとLedgerEntry・Purchase・Observation・financial Node batch、Household Query・membership、Budget・Recurring Expense Query/Mutation、Allocation Rule・Funding Schedule Query、SetAllocationRule・SetFundingSchedule mutation、口座作成はApplication Portを経由する。

未解決の境界は、GraphQL内のDiesel Query (`node_batch_catalog_planning.rs`、`subscriptions.rs`)、Pool funding rule・Pool creation・default allocation、Household lifecycle、金融系Command、Purchase・Statement・EvidenceのCommandとRead model、GraphQLから行うcross-context補助Queryである。Nodeの台帳・購入batchは主QueryをPortへ移したが、結果補完のためPostgres adapterを直接呼び出している。これらをApplication契約とPortへ移し、GraphQLから`*-postgres`・Diesel・PgPoolへの直接依存を除く必要がある。

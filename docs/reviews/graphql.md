~/ghq/github.com/walnuts1018/piccaのGraphQL Schemaを真似して改善を行って。

- GraphQL Schemaの適切なファイル分離（1mutation or 1 type(+それに関連する定義)）で1ファイルにする
- Relay/gqlgenを前提としてもっと良いGraphQL Schemaにして。良いGraphQL Schemaとは以下のようなものです。
  - Frontendでの複雑な条件分岐が少ない
  - 適切に抽象化されており、nullableなフィールドが最低限に抑えられている
  - ドメインモデリングが適切に行われており、Relay Server Schemaに沿った設計になっている
- Mutationの戻り値はunionを用いたResult型にする
- interfaceやunionを適切に活用する
- Relayの機能をしっかり積極的に活用する

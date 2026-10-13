# Horse バックエンドの保守性・ライブラリ活用レビュー

対象は [walnuts1018/horse](https://github.com/walnuts1018/horse) の `main`、commit [`be514744`](https://github.com/walnuts1018/horse/commit/be514744182bd6cc1ffe1847ea588b801c2db894) です。

結論として、技術スタックの選択は適切ですが、バックエンドの責務分割には引き続き改善の余地があります。購入処理と送金・返済処理はApplication層へ移行済みですが、精算・訂正などの処理はGraphQL Resolver側に残っています。

一方、すでに改善されている箇所もあります。

- `gqlgen`は`follow-schema`構成になり、GraphQL SDLが機能別に分割されています。
- `Bob`の生成モデルとリレーションローダーを利用しており、購入一覧の取得も一括ロード方式です。
- `google.golang.org/genai`の公式SDKを導入済みです。
- 購入仕訳の構築は`internal/application/accounting`に共通化され始めています。
- `pgx`、Atlas、OpenTelemetry、ZITADEL OIDCなど、必要な基盤ライブラリはすでに揃っています。

したがって、ORMやGraphQLフレームワークを交換したり、大きなDIフレームワークを追加したりする必要はありません。 今あるライブラリの機能をより深く活用し、自前実装と重複コードを減らす方向が最適です。

以下は、現時点のコードから具体的に確認できた改善事項です。

## 1. 最優先：購入処理の更新経路を一本化する

優先度：高 / 現在のコードに不整合あり

該当ファイル：

- [internal/application/purchase/post.go](https://github.com/walnuts1018/horse/blob/be514744182bd6cc1ffe1847ea588b801c2db894/internal/application/purchase/post.go)
- [internal/graph/mutations.go — syncPostedPurchaseAccounting](https://github.com/walnuts1018/horse/blob/be514744182bd6cc1ffe1847ea588b801c2db894/internal/graph/mutations.go#L899-L990)
- [internal/graph/member_burden.go](https://github.com/walnuts1018/horse/blob/be514744182bd6cc1ffe1847ea588b801c2db894/internal/graph/member_burden.go)

`post.go`の購入確定処理は、購入ごとに以下を実行しています。

```
snapshotMemberBurden(...)
snapshotPaymentFunding(...)
accounting.BuildPurchaseEntries(...)
accounting.InsertEntries(...)
```

しかし、確定済み購入の編集後に呼ばれる`syncPostedPurchaseAccounting`では、仕訳とメンバー負担は更新するものの、`purchase_payment_funding_snapshots`を再作成していません。

さらに、決済内訳の置換処理では古い`purchase_payments`を削除して再挿入します。DB上では支払元スナップショットが`ON DELETE CASCADE`で結び付いているため、決済内訳の編集後にスナップショットが失われ、精算計算が失敗する経路があります。

加えて、`refreshPurchaseMemberBurden`には次の処理があります。

```
weights := weightsByAllocation[key]
if len(weights) == 0 {
    continue
}
```

確定後の編集で新しい負担プールへ配賦する場合、以前のスナップショットが存在しないため、負担情報を保存せず通過してしまいます。

つまり、根本原因は購入確定時と更新時で別々の会計同期ロジックを持っていることです。

推奨する変更は、`purchase.Service`に購入の作成・確定・更新・訂正の操作を集約し、仕訳と負担スナップショットの同期を共通の処理で保証することです。

ただし、更新時に何でも現在の負担ルールで再計算するのは避けてください。既存の配賦に対しては過去のルールを保持し、新たな配賦には有効なルールを明示的に適用する必要があります。

また、`snapshotMemberBurden`と`snapshotPaymentFunding`には、ルールが見つからない場合などに`continue`する箇所があります。これは会計上の必須情報なので、未設定として明示的に失敗させる方針へ変更すべきです。

## 2. Application層への移行が中途半端で、GraphQL Resolverが肥大化している

優先度：高 / 保守性への影響が大きい

現在の`internal/graph/mutations.go`は約3,080行あります。

購入作成・確定と送金・返済はApplication層へ移動済みです。次の機能はGraphQL Resolver配下に会計・永続化ロジックが混在しています。

| 処理 | 現在の主な実装先 |
| --------- | --------------------------------------------- |
| 購入作成・確定 | `internal/application/purchase` |
| 購入編集・品目編集 | `internal/graph/mutations.go` |
| 月次精算・締め処理 | `internal/graph/accounting.go`、`mutations.go` |
| 入金 | `internal/graph/mutations.go` |
| 送金・返済 | `internal/application/transfer` |
| 口座間移動・収入 | `internal/graph/account_movements.go` |
| 予算・定期支出 | `internal/graph/planning.go` |

GraphQLからしか呼ばないコードであっても、数百行のトランザクション・仕訳処理をResolverに置くのは避けたいです。

以下のような構成へ整理することを推奨します。

```
internal/
  domain/
    allocation/
    settlement/
    money/
  application/
    purchase/
    funding/
    transfer/
    settlement/
    catalog/
  postgres/
    generated/
    queries/
  graph/
    ...
  ingestion/
    ...
```

これは新しい巨大なレイヤードアーキテクチャを導入するという意味ではありません。業務上ひとまとまりの操作を小さなServiceに移し、GraphQL Resolverは入力変換・認可入口・結果変換に集中させるだけです。

とりわけ、GraphQLの型をApplication層で使わず、現在の`purchase.CreateCommand`のような独立したコマンド型を使用する方針は正しいので、そのまま他の業務へ展開するのがよいです。

### 重複しているコード

現在、`internal/application/purchase`と`internal/graph`には、それぞれ次のような役割の関数が存在します。

| 重複する責務      | 例                                                    |
| ----------- | ---------------------------------------------------- |
| 負担ルール解決     | `allocationRuleWeights`                              |
| 決済情報の検証・保存  | `validatePayments` / `validatePurchasePaymentInputs` |
| 会計期間の準備     | `ensureAccountingPeriod`                             |
| 家庭イベント記録    | `writeEvent`                                         |
| 冪等キー検査      | `validateIdempotencyFingerprint`                     |
| UUID変換・金額加算 | `parseUUID`、`checkedAdd`など                           |

これらは単に行数が増えるだけでなく、片方の修正がもう片方へ反映されない原因になります。

`internal/application/accounting`の仕訳構築と同様、共有できる操作は明示的な小さなパッケージへ集約してください。

一方、単純な数行の文字列変換まで、すべて共通ヘルパー化する必要はありません。会計上の不変条件やトランザクション境界を持つ処理を優先すべきです。

## 3. Bobの機能をもっと活用できる

優先度：高 / 既存ライブラリの活用余地

現在の`bobgen.yaml`は次の設定です。

```
plugins:
  factory:
    disabled: true
  queries:
    disabled: true
```

BobのORMとリレーションローダーは使えていますが、FactoryとSQL Query Code Generationの両方が無効になっています。

### 3.1 Bob Factoryは有効化することを推奨

[bobgen.yaml](https://github.com/walnuts1018/horse/blob/be514744182bd6cc1ffe1847ea588b801c2db894/bobgen.yaml)

統合テストを見ると、ほぼ毎回次のようなセットアップを行っています。

```
Member作成
  ↓
Household作成
  ↓
HouseholdMember作成
  ↓
Pool作成
  ↓
FundingRule設定
  ↓
Account作成
  ↓
Purchase作成
```

例えば`accounting_integration_test.go`や`products_integration_test.go`では、かなりの行数が検証そのものではなく、DBデータを用意するコードになっています。

Bob Factoryを利用すると、DBの外部キー関係や必須カラムを理解したモデル生成ができ、テストデータの作成量を削減できます。DBに保存する`Create`系だけでなく、保存せずにオブジェクトを組み立てる`Build`系もあります。

[image](https://www.google.com/s2/favicons?domain=https://bob.stephenafamo.com\&sz=32)

Bob

+1

おすすめは、Factoryを有効にしたうえで、業務的に意味のあるシナリオだけ薄いFixtureを作る方式です。

例えば次のようなAPIです。

```
scenario := testfixture.NewHousehold(t, db).
    WithMembers("A", "B").
    WithSharedPool(3, 2).
    WithPersonalAccounts().
    WithPurchase(1200)
```

これは設計例であり、Bob Factoryがこのメソッドを直接生成するわけではありません。

FactoryにDB行の生成を担当させ、`testfixture`が「共有家計として正しい状態」の構築を担当します。

ただし、テスト対象が「購入登録のMutation自身」であれば、そこはFactoryで直接購入を作らず、実際のMutationを呼んで検証する必要があります。

### 3.2 SQL Query Code Generationは複雑な集計に選択的に使う

Bobには手書きSQLから型付きGoコードを生成する仕組みもあります。

[image](https://www.google.com/s2/favicons?domain=https://bob.stephenafamo.com\&sz=32)

Bob

これは全クエリに使う必要はありません。

現在のBobモデルによるCRUDや、`monthly_spending.go`で利用しているBobのQuery Builderは、そのままで十分です。

一方、複雑な精算・集計SQLをGoの中で大量のQuery Builder呼び出しとして組み立てると、SQL全体の理解や実行計画の確認が難しくなります。

特に以下は、名前付きSQLとして管理する価値があります。

| クエリ | 理由 |
| -------------------- | -------------- |
| 月次精算の購入・決済・負担情報の一括取得 | 多数の関連テーブルを横断する |
| 商品検索候補の抽出・ランキング | インデックスと実行計画が重要 |
| 口座残高の期間別集計 | 集約ロジックがSQL中心 |
| 月次・年次の支出分析 | 集計条件が複雑 |

Bob生成モデルはCRUDに使い、複雑な読み取りだけSQLファイルとして明示する構成が、保守性と性能のバランスに優れています。

## 4. 精算処理をApplication層へ移行する

優先度：高 / 保守性への影響

`loadFundingExpenses`は購入品目・決済・負担スナップショット・負担履歴を購入ID最大500件単位でまとめて取得するため、購入ごとにクエリを発行するN+1は解消されています。

一方、`calculateSettlementPreviewWithRules`は最後に締めた月から対象月まで各月の精算を個別に再計算します。長期間締めていない家庭では、対象期間の月数に応じて同じ基礎データを繰り返し読み込みます。また、精算の取得と計算は引き続きGraphQL Resolver配下にあります。

精算処理をApplication層へ移す際は、期間内の購入・決済・負担スナップショット・訂正・予定入金・実入金をまとめた入力を用意し、純粋な精算計算とDB読み込みを分けてください。複数月のプレビューでは、期間ごとの読み込みを再利用できる形にしてください。

## 5. トランザクションと共通処理をもっと簡潔にできる

現在のMutationには、次のパターンが多数あります。

```
tx, err := r.DB.Begin(ctx)
if err != nil {
    return nil, err
}
defer func() {
    _ = tx.Rollback(context.WithoutCancel(ctx))
}()

// 大量のDB処理

if err := tx.Commit(ctx); err != nil {
    return nil, err
}
```

この方式自体は正しく、必ずしも問題ではありません。

ただ、業務処理のたびに開始・Rollback・Commitを書いていると、ロック取得順序や例外時の扱いが分散します。

`pgx.BeginFunc`や`pgx.BeginTxFunc`を使うと、コミット・ロールバック処理を共通化できます。

[image](https://www.google.com/s2/favicons?domain=https://pkg.go.dev\&sz=32)

Go Packages

```
err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
    return service.Execute(ctx, tx, command)
})
```

ただし、今のコードではキャンセルされたContextをRollbackへ渡さないよう工夫しています。そのため、単純に全箇所を`BeginFunc`へ機械的に置換するのは避け、Contextキャンセル時の接続解放動作も確認してください。

私なら、`pgx`を使用した薄いトランザクション実行ヘルパーを共通化して、Serviceは`bob.Executor`または`pgx.Tx`を受け取る形にします。

大きなUnit of Workフレームワークを追加する必要はありません。

## 7. 取り込みWorkerも責務を分けたい

優先度：中

[internal/ingestion/worker.go](https://github.com/walnuts1018/horse/blob/be514744182bd6cc1ffe1847ea588b801c2db894/internal/ingestion/worker.go)

`worker.go`は約600行あり、`RunOne`には次の責務が混在しています。

| 責務 | 改善方法 |
| ----------------------- | ----------------------- |
| PostgreSQLからジョブをClaimする | `JobRepository`へ分離 |
| 期限切れジョブを復旧する | `JobRepository`へ分離 |
| 原本を読み取る | `DocumentStore`のまま維持 |
| OCR・Gemini・履歴検索を呼ぶ | `ExtractionPipeline`へ分離 |
| 解析結果を検証する | 純粋な検証関数として維持 |
| ジョブ完了・失敗をDBへ保存する | `JobRepository`へ分離 |

すでにRabbitMQの通知とPostgreSQLのジョブ永続化が分離されている点はよい設計です。

ここでTemporalなどの新しいワークフローエンジンを導入する必要はありません。現在の処理規模なら、既存のRabbitMQとPostgreSQLで十分です。

また、Gemini部分は公式`google.golang.org/genai` SDKへ移行済みなので、以前のような生HTTPクライアントへの置換提案は不要です。

ただし、API呼び出しのたびに`genai.NewClient`を生成しているため、将来高並列で処理するなら、無料・有料ルートそれぞれのクライアントを再利用できる設計を検討できます。現時点では優先度は高くありません。

## 8. GoのLinterももう少し厳しくしたい

現在の`.golangci.yml`は比較的シンプルです。

```
linters:
  default: standard
  enable:
    - bodyclose
    - errorlint
    - gosec
```

Go 1.27とgolangci-lint 2.14を使っているので、保守性に直接効くLinterを追加する価値があります。

| Linter       | 目的                | 推奨             |
| ------------ | ----------------- | -------------- |
| `gocritic`   | 疑わしい実装・非効率な書き方の検出 | 導入             |
| `revive`     | コード品質・API設計の一貫性   | 導入             |
| `unparam`    | 不要な引数や固定的な戻り値の検出  | 導入             |
| `gocognit`   | 複雑すぎる業務関数の発見      | 閾値を調整して導入      |
| `exhaustive` | enum相当のswitchの網羅性 | 対象を絞って導入       |
| `gofumpt`    | フォーマットの統一         | `gofmt`から切替を検討 |

golangci-lint 2.14はGo 1.27に対応しており、これらのLinterの多くもサポートされています。

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

ただし、`generated.go`、Bob生成モデルなどは適切に除外し、人間が保守するコードに対して有効にするのが前提です。

また、Linterを強化することで大量の形式的な警告が増えるなら、無理に全ルールを有効にすべきではありません。

## 9. ライブラリ採用の最終判断

| 技術・ライブラリ | 判断 | 理由 |
| ----------------------- | ------- | ----------------------------- |
| Bob | 維持・活用強化 | Factory、一括Insert、関係ロードが有用 |
| Bob Query Generation | 部分採用 | 複雑な精算・集計SQLに適する |
| gqlgen | 維持 | Relay向けのスキーマ分割・Node対応を進められている |
| pgx | 維持・共通化 | トランザクション、DBアクセスに十分 |
| Atlas | 維持 | DBマイグレーション管理に適切 |
| google.golang.org/genai | 維持 | すでに公式SDKを使用 |
| OpenTelemetry | 維持 | 既存の観測基盤と整合 |
| testify | 積極導入 | |

## 推奨する修正順序

| P0 | 購入確定・更新・訂正の会計同期処理を一本化し、負担スナップショット欠落を修正 |
| -- | ------------------------------------------ |
| P1 | `mutations.go`の会計処理をApplication Serviceへ移動 |
| P1 | 精算処理のN+1クエリを一括読み取りへ変更 |
| P1 | Bob Factoryを有効化し、統合テストのFixtureを共通化 |
| P2 | トランザクション・イベント・冪等性処理を共通化 |
| P2 | 複雑な読み取りにBob Query Generationを活用 |
| P2 | Relay `nodes`のバッチ化、商品検索クエリの整理 |
| P2 | WorkerのDBジョブ管理と推論パイプラインを分離 |
| P3 | 共通エラー型、UUID処理、Linterを整理 |

なお、現在のCIはPostgreSQL 18を立ち上げてマイグレーションと統合テストを実行する構成になっています。そのため、統合テスト自体がないという評価は当たりません。改善対象は、テストデータのセットアップの重複、未設定ルールや購入更新の異常系、精算の大量データに対する計測です。

最も大きな改善効果が期待できるのは、ライブラリを大量に追加することではなく、すでに利用しているBob・gqlgen・pgxの強みを活かしながら、会計の業務ロジックをGraphQL層から分離することです。

今回の指摘は最新コミットのソースコードを調べた静的レビューに基づきます。修正に際しては、特に決済内訳変更後のスナップショット・精算結果の回帰テストを先に追加してから、処理を共通化するのが安全です。

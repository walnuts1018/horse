# Horse バックエンドの保守性・ライブラリ活用レビュー

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse) 確認コミット：`2f3b983e6327e1781400175fd1f31893b373307c`（2026年10月13日）

## 総評

現状のバックエンドでは、ResolverからApplication Serviceへの移行やBobの活用が進んでいます。一方で、会計ユースケース以外のResolverには業務処理とトランザクション管理が残り、読み取り処理もGraphQL層に配置されています。

現時点で残っている主な課題は次のとおりです。

1. 一部のMutationに業務ロジック・DB操作・トランザクション管理が残っている。
2. Gemini SchemaとReceipt型は手作業で管理されている。
3. pgxのトランザクション管理とApplication Serviceへの移行が一部にとどまっている。
4. 読み取り処理の配置とDBトレーシングが未整理である。
5. SSEが1秒間隔でDBをポーリングしている。

ただし、何でも新しいライブラリに置き換えればよいわけではありません。

| 領域            | 現在の選択              | 判断           |
| ------------- | ------------------ | ------------ |
| API           | Go + gqlgen        | 維持           |
| DBアクセス        | Bob + pgx          | 維持・活用拡大      |
| DBマイグレーション    | Atlas              | 維持           |
| 認証            | go-oidc + x/oauth2 | 維持           |
| S3            | AWS SDK for Go v2  | 維持           |
| メッセージング       | amqp091-go         | 維持           |
| 監視            | OpenTelemetry      | 維持           |
| Gemini        | Go公式SDK              | 維持           |
| トランザクション・業務処理 | Resolver内への直接実装    | Service層へ移動  |

重要なのは、今のGoバックエンドを別のフレームワークへ移行することではなく、すでに採用しているライブラリを活かし、責務を正しく分離することです。

## 1. Mutationの責務分離

優先度：高 / 保守性

現状確認では、以前の`mutations.go`は機能別ファイルへ分割され、購入作成・確定、入金、送金、精算のApplication Serviceが追加されています。購入作成・確定は`internal/application/purchase`、入金は`internal/application/funding`、送金は`internal/application/transfer`、精算は`internal/application/settlement`が処理します。

一方、購入訂正は[mutations_purchase_correction.go](../../internal/graph/mutations_purchase_correction.go)でDBトランザクションと業務処理を実行し、品目更新も[update_purchase_item.go](../../internal/graph/update_purchase_item.go)で同様に処理します。Resolverの分割とService移行は進んでいますが、ユースケース全体の責務分離は未完了です。

### 推奨する構造

```
internal/
├── graph/
│   ├── resolver.go
│   ├── purchase.resolvers.go
│   └── settlement.resolvers.go
├── application/
│   ├── purchase/
│   │   ├── service.go
│   │   ├── command.go
│   │   └── service_test.go
│   ├── settlement/
│   │   └── service.go
│   ├── funding/
│   │   └── service.go
│   └── importdocument/
│       └── service.go
├── domain/
│   ├── allocation.go
│   ├── accounting.go
│   └── settlement.go
├── postgres/
│   ├── purchase.go
│   ├── settlement.go
│   └── generated/
└── ingestion/
```

`graph`にはGraphQL固有の処理を残し、`application`でユースケースを実行し、`domain`で純粋な会計計算を行う構成です。

例えばResolverは次の程度で十分です。

```
func (r *mutationResolver) CreatePurchase(
    ctx context.Context,
    input model.CreatePurchaseInput,
) (*model.Purchase, error) {
    actor, err := principal(ctx)
    if err != nil {
        return nil, err
    }

    purchase, err := r.PurchaseService.Create(
        ctx,
        actor.MemberID,
        toCreatePurchaseCommand(input),
    )
    if err != nil {
        return nil, err
    }

    return toGraphPurchase(purchase), nil
}
```

Service層ではGraphQLの生成モデルを使わず、アプリケーション独自のCommand・Resultを使います。

こうすることで、将来的にレシート取り込みWorkerから購入を確定する際も、GraphQL Resolverを経由せず同じ業務処理を再利用できます。

なお、Repositoryインターフェースをテーブルごとに大量生産する必要はありません。 Goでは不要な抽象化も保守性を悪化させます。まずはユースケース単位のServiceと、必要な場所だけの小さなインターフェースで十分です。

## 2. Gemini Schema定義とGo型の同期

優先度：高 / ライブラリ活用

Geminiとの通信は`google.golang.org/genai`へ移行済みです。また、`TestReceiptSchemaMatchesJSONTypesAndRequiredFields`がReceipt型と手作りSchemaのプロパティ・型・必須項目を照合します。

一方、[gemini.go](../../internal/ingestion/gemini.go)の`receiptSchema()`は引き続き`map[string]any`で定義されており、Go型との二重管理は残っています。Contract Testは型とSchemaのずれを検出しますが、Schemaの生成やGemini固有制約の検証までは行いません。

## 3. pgxのトランザクション管理とService層への移行

優先度：中〜高 / 保守性

GraphQL層には現在も、次の手動トランザクション管理を使うMutationが複数残っています。例えば購入訂正は`internal/graph/mutations_purchase_correction.go`、品目更新は`internal/graph/update_purchase_item.go`でこの形を使います。

```
tx, err := r.DB.Begin(ctx)
if err != nil {
    return nil, err
}
defer func() {
    _ = tx.Rollback(context.WithoutCancel(ctx))
}()

// 複数のDB操作

if err := tx.Commit(ctx); err != nil {
    return nil, err
}
```

これは誤った実装ではありません。

リポジトリ内では`pgx.BeginFunc`と`pgx.BeginTxFunc`を使用しておらず、GraphQL・Application Serviceの双方で`Begin`、`Commit`、`Rollback`を手動管理しています。pgxのコールバックAPIを使えば成功・失敗時の完了処理をまとめられますが、`context.WithoutCancel`によるRollbackの挙動を保つ必要があります。

例えば、

```
err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
    if err := validatePeriod(ctx, tx, period); err != nil {
        return err
    }
    if err := savePurchase(ctx, tx, purchase); err != nil {
        return err
    }
    return saveJournal(ctx, tx, journal)
})
```

のように書けます。

成功時にコミット、失敗時にロールバックされるため、トランザクション処理の記述が簡潔になります。

ただし、リクエストキャンセル後も確実にRollbackを試みる必要がある箇所では、そのコンテキスト設計を維持してください。

現在のコードは`context.WithoutCancel(ctx)`を使用しているので、変更時にはキャンセルとタイムアウトの挙動も確認するべきです。

### トランザクションの境界はService層で管理する

さらに重要なのは、Transactionをどの層が所有するかです。

購入・入金・送金・精算のServiceは自らTxを開始しますが、前述の購入訂正や品目更新などはGraphQL ResolverがTxを開始します。Service層がトランザクション境界を所有する設計は一部のみ適用されています。

例えば購入確定は、購入情報の保存、配賦、仕訳、EvidenceLink、イベントまで同じトランザクションで処理します。

一方、S3の原本保存やGemini APIへの呼び出しは、DBトランザクションの外側で実行します。

DBロックを保持したまま外部APIを待つ構成にはしないでください。

## 4. 読み取り専用APIを専用Query層に分離する

優先度：中〜高 / 設計・性能

読み取り処理の一部はGraphQLパッケージに残っています。例えば`monthly_spending.go`と`account_balances.go`はBob DSLを使いますが、専用Query層には分離されていません。商品候補検索は`internal/postgres/query`へ移しました。

具体的には以下です。

| 処理 | 現状の問題 | 推奨 |
| ------------------------- | ---------------------- | ------------ |
| `monthlySpending` | DB集計と表示用集計がGraphQLパッケージにある | Bobの集計Queryと結果変換を専用Query層へまとめる |
| `accountBalanceAnalysis` | 仕訳取得と残高分析がGraphQLパッケージにある | 読取条件と残高計算を専用Query層へまとめる |

これらの処理には、CQRSのような大掛かりなフレームワークは不要です。

単純に、

```
postgres/
├── purchase_queries.go
├── spending_queries.go
├── account_queries.go
└── product_queries.go
```

のようなQuery層を作り、Bob生成Queryとモデル変換をまとめれば十分です。

これによってGraphQLの変更とBob DSLの最適化を独立して進められます。

## 5. OpenTelemetryとテスト基盤

### PostgreSQLのトレースを取得する

`cmd/api/main.go`と`cmd/worker/main.go`はOpenTelemetry SDKを初期化し、APIは`otelhttp`でHTTP計測を行います。一方、両方のDBプールは`pgxpool.New`で作成され、pgxのトレーサーは設定されていません。`go.mod`にも`otelpgx`はありません。

ここには otelpgx が適しています。

`otelpgx.NewTracer()`をpgxに設定すれば、SQLの実行時間やコネクション取得をTraceへ追加できます。DBプールの統計も収集可能です。

これを導入しておくと、今回指摘したN+1や遅いSQLを実際のトレースから特定できます。

SQL全文やパラメータを不用意に収集しないよう、計測属性の設定には注意してください。

### テスト用Factoryを検討する

`bobgen.yaml`ではFactory生成が無効のままです。統合テストでは家庭・口座・会計条件などをテストごとに明示的に作成しています。Factoryで重複を減らす余地はありますが、会計条件を暗黙化しないよう、共通準備とテスト固有データの境界を決める必要があります。

```
factory:
  disabled: true
```

ただし、会計テストでは正確な金額や負担関係が重要です。

Factoryであらゆる値を暗黙に補完するより、家庭・メンバー・プール・口座などの共通準備だけをFactoryに任せ、金額と精算条件はテスト内で明示する設計がよいです。

現状のCIではPostgreSQL 18を起動してDBマイグレーションと統合テストを実行しているので、テストのために別のデータベースやTestcontainersへ切り替える必要はありません。

## 6. 追加ライブラリなしで改善できるところ

すべてをライブラリで解決する必要はありません。

現在の`internal/api/server.go`の`events`ハンドラは、接続ごとに1秒間隔で`household_events`を問い合わせています。GraphQLの購入Subscriptionも1秒間隔で変更を確認します。カーソルを使った再取得は実装されていますが、待機中の接続数に応じて定期的なDB問い合わせが増えます。

複数ユーザーでの同時編集を前提にすると、将来的に無駄なDBアクセスが増える構造です。

ここはpgxの`LISTEN/NOTIFY`を使ってイベント到着を通知し、既存の`household_events`のカーソルで確実に取りこぼしを再取得する方式に変更できます。

新しくRedis Pub/SubやWebSocketライブラリを追加する必要はありません。

ただし、家庭内で数人が接続する程度なら現状のポーリングでも十分な可能性があります。クエリ数を計測してから改善してもよい部分です。

また、`log/slog`、`net/mail`、`mime/multipart`、`slices`など、現状で標準ライブラリを活用している部分はそのまま維持するのが適切です。

## ライブラリ採用の最終判断

| ライブラリ・機能 | 判断・理由 |
| ------------------------- | ------------------ |
| Bob ThenLoad / Preload | 購入一覧などで採用済み |
| Bob Query Builder | 検索・集計で採用済み |
| `google.golang.org/genai` | 採用済み — Gemini APIの通信に使用 |
| `pgx.BeginFunc` | 未採用 — 現在の`context.WithoutCancel`を使うRollbackと同じ挙動を保てる形が確認できていないため、手動のTx管理を継続 |
| `otelpgx` | 未採用 — DBプールにpgx tracerを設定しておらず、SQL traceやプール統計を取得する運用要件も未確認 |
| Bob Factory | 採用検討 — テストデータ生成 |
| gqlgen DataLoader | Node参照には独自のバッチ処理を実装済み。追加導入は必要性を個別に判断 |
| 別のORM（GORM・Ent） | 不要 — Bobで十分 |
| 別のHTTPフレームワーク | echo/v5を用いる |
| DIコンテナ | kessokuを用いる |

## 実装する順番

| 順位 | 変更 | 効果 |
| -- | --------------------------------- | -------------- |
| 1 | 購入訂正・品目更新などをApplication Serviceへ移行 | GraphQLから業務処理を分離 |
| 2 | DBトランザクションの境界をService層へ統一 | Tx管理の一貫性 |
| 3 | 読み取り処理をQuery層へ分離 | API変更と検索最適化の独立性 |
| 4 | otelpgxによるSQL計測 | DB処理の性能問題の発見 |
| 5 | Gemini Schemaの生成または契約検証を強化 | 型定義との同期維持 |
| 6 | SSEの通知をLISTEN/NOTIFYへ変更 | 接続数に応じた定期クエリの削減 |
| 7 | 共通テスト準備のFactory化を評価 | テストデータ作成の重複削減 |

### 最終的な評価

GraphQLパッケージには、購入訂正・品目更新などの業務処理と読み取り処理が残っています。すでに導入されたApplication Serviceと会計ロジックの共通化を、残りのユースケースにも適用し、GraphQLを入出力変換に集中させることが今後の課題です。

この監査では現行コードを静的に確認しました。DB性能の実測は行っていません。N+1と購入仕訳生成の重複は解消されているため、今後は残存ResolverのService移行、DBトレーシング、読み取り層の整理を優先します。

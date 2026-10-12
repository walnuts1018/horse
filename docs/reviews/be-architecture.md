# Horse バックエンドの保守性・ライブラリ活用レビュー

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse) 確認コミット：`2f3b983e6327e1781400175fd1f31893b373307c`（2026年10月13日）

## 総評

現状のバックエンドは、技術スタックの選定は適切ですが、ライブラリが提供している機能を十分活用しきれておらず、アプリケーション固有の処理がGraphQL Resolverに集中しています。

特に改善したいのは次の5点です。

1. GraphQL Resolverに業務ロジック・DB操作・トランザクション管理・レスポンス変換が混在している。
2. Bobのリレーションロード機能を十分使わず、N+1クエリを発生させている。
3. Gemini APIのHTTPクライアントを独自実装しており、公式SDKに任せられる処理を多数抱えている。
4. 会計処理に似たコードが複数存在し、今後の修正時に整合性を崩しやすい。

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
| Gemini        | 独自REST実装           | 公式SDKへの移行を推奨 |
| トランザクション・業務処理 | Resolver内への直接実装    | Service層へ移動  |

重要なのは、今のGoバックエンドを別のフレームワークへ移行することではなく、すでに採用しているライブラリを活かし、責務を正しく分離することです。

## 1. GraphQL Resolverが巨大すぎる

優先度：高 / 保守性

該当：[internal/graph/mutations.go](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/internal/graph/mutations.go)

現在の`mutations.go`は約3,570行あります。自動生成ファイルではなく、手書きの実装です。

特に長い関数には以下があります。

| 関数 | おおよその行数 |
| -------------------- | ------- |
| `createPurchase` | 298行 |
| `recordFunding` | 244行 |
| `recordTransfer` | 386行 |
| `correctPurchase` | 378行 |
| `updatePurchaseItem` | 390行 |

問題は単純に行数が多いことではありません。

例えば`createPurchase`では、入力検証、認可、冪等性確認、会計期間ロック、負担ルール検索、商品情報検証、データ保存など、異なる責務が1つの関数に存在しています。

`recordTransfer`や`correctPurchase`も同様です。

この状態では、会計ロジックを修正するときにGraphQLの実装詳細まで理解する必要があり、API以外から同じ処理を再利用することも難しくなります。

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

## 2. Bobを使っているのにN+1クエリが多い

優先度：高 / 性能・保守性

ここはかなり明確な改善対象です。

### 購入一覧の取得

該当：

- [queries.go: purchases](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/internal/graph/queries.go#L159-L261)
- [helpers.go: loadPurchase](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/internal/graph/helpers.go#L229-L441)

購入一覧では、まず購入を取得してから、各購入に対して`loadPurchase`を呼んでいます。

さらに`loadPurchase`内部で、品目ごとに配賦・タグなどを検索しています。

```
for _, row := range purchases {
    purchase, err := loadPurchase(
        ctx, r.DB, row.ID.String(),
    )
    // ...
}
```

そして`loadPurchase`内部にも次のような処理があります。

```
for _, item := range items {
    allocations, err := models.ItemAllocations.Query(
        // itemごとの条件
    ).All(ctx, executor)
}
```

これは典型的な多段N+1です。

1回の一覧取得で50件の購入、各10品目を表示するような場合、関連データの取得だけで大量のSQLを発行する構造になっています。

### BobのThenLoadを活用する

Bobには、関連モデルをまとめて取得する`ThenLoad`と、JOINによる`Preload`があります。

[image](https://www.google.com/s2/favicons?domain=https://bob.stephenafamo.com\&sz=32)

Bob

現状でも`LoadMember`や`LoadAccount`は一部使用されていますが、親の集合全体に対してまとめてロードする設計に統一できていません。

推奨するのは次の処理です。

```
購入一覧
  ↓ 1クエリ
対象購入の品目
  ↓ 1クエリ
対象品目の配賦
  ↓ 1クエリ
対象品目のタグ
  ↓ 1クエリ
購入単位の決済・訂正・証拠
  ↓ 数クエリ
レスポンスを組み立て
```

必ずしもすべて1クエリに統合する必要はありません。多対多の関係を無理にJOINすると行数が膨張するからです。

関連データの種類に応じて数回のバッチクエリで取得する構造に変えるのがよいです。

gqlgenでDataLoaderを導入する方法もありますが、現在の実装ではResolverがGraphQLのフィールドを処理する前に`loadPurchase`が一括でモデルを組み立てています。そのため、DataLoaderを追加するだけでは既存のN+1は解消されません。

まずBob側のバッチ取得を改善し、必要に応じてフィールドResolver用のDataLoaderを採用してください。

### 月次分析も同様

[月次支出分析の実装](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/internal/graph/queries.go#L388-L624)

`monthlyCategorySpending`は購入ごとに品目と訂正を取得し、Goで集計しています。

`monthlyMemberSpending`では、購入ごとに配賦、負担スナップショット、訂正を取得し、さらに訂正ごとにメンバー負担を取得しています。

この種の集計はBob DSLの集計式とJOINでDB上に構築し、Goでは会計ルールと結果の整形を扱うべきです。

## 3. Bob DSLで集計と検索を型安全に実装する

優先度：高 / ライブラリ活用

該当：[bobgen.yaml](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/bobgen.yaml)

現在は以下の設定になっています。

```
queries:
  disabled: true
```

このプロジェクトでは、文字列としてSQLを書く方法を採用せず、Bob DSLで型安全な検索・集計を組み立てます。

| 用途 | 推奨 |
| --------- | ------------------------- |
| 単純なCRUD | Bob生成Model |
| 動的な検索条件 | Bob Query Builder |
| リレーション取得 | Bob ThenLoad / Preload |
| 購入一覧の集約取得 | Bob Query BuilderとThenLoad |
| 月次支出分析 | Bob DSLによる集計 |
| 口座残高集計 | Bob DSLによる集計 |
| 複雑な会計計算 | Goのdomain関数 |

現行の月次集計や口座分析は会計仕様を反映したBob DSLに置き換え、追加の集計も同じ型安全な構成にそろえます。DB固有の実行計画を確認するときは、Bobが生成するクエリと引数を使って検証します。

## 4. 会計仕訳の構築ロジックが重複している

優先度：高 / 会計の正確性

次の2つは、かなり似た処理を別々に実装しています。

- [syncPostedPurchaseAccounting](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/internal/graph/mutations.go#L1193-L1328)
- [postPurchase](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/internal/graph/mutations.go#L1921-L2132)

どちらも、

- 品目金額の合計
- プール別負担金額の集約
- 仕訳行番号の採番
- 借方・貸方の設定
- 決済口座の仕訳作成

といった処理を行います。

問題は、将来片方の実装だけを修正してしまうことです。

例えば決済に新しい種類を追加した際、購入確定では正しく処理できるのに、購入編集後の仕訳再生成では反映されない、といったバグを生みやすくなります。

### 修正案：仕訳生成を純粋関数にする

```
type JournalLine struct {
    AccountID *uuid.UUID
    PoolID    *uuid.UUID
    DebitYen  int64
    CreditYen int64
    Memo      string
}

func BuildPurchaseJournal(
    purchase PurchaseAccountingSnapshot,
) ([]JournalLine, error)
```

このような関数を`domain`または会計のapplication層に実装します。

購入の確定も編集も、同じ関数で仕訳内容を作成し、DBへの保存方法だけを分けます。

このロジックは家計簿の中核なので、あえてライブラリに依存させる必要はありません。

配賦・金額の保存則・仕訳構築は自前の純粋関数、DBアクセスとトランザクションはBob/pgxという役割分担が最適だと思います。

## 5. Gemini APIの独自HTTP実装を公式SDKへ置き換える

優先度：高 / ライブラリ活用

該当：[internal/ingestion/gemini.go](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/internal/ingestion/gemini.go)

現在の`Gemini.ExtractRun`では、以下の処理を自前で実装しています。

- HTTPリクエストとJSONペイロードの構築
- 画像のBase64エンコード
- Thinking設定とService Tierの指定
- レスポンスのJSONデコード
- 候補とFinishReasonの解析
- トークン使用量の取得
- HTTPエラーの解釈

このうち大部分は、Google公式のGo SDKに任せられます。

### `google.golang.org/genai`を採用する

公式Go SDKは現在、必要な次の機能を備えています。

| 機能                              | SDK対応 |
| ------------------------------- | ----- |
| 画像・PDF・テキスト入力                   | 対応    |
| Structured Output / JSON Schema | 対応    |
| Thinking Level                  | 対応    |
| Standard / Flex Service Tier    | 対応    |
| Batch API                       | 対応    |
| Token Usage取得                   | 対応    |
| Files API                       | 対応    |

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

+3

特に重要なのは、Free/Paidのモデルルーティングは今までどおり自前で実装し、Geminiへの通信部分だけSDKに任せることです。

今回のFree/Paid切り替え、予算管理、品質に応じた再解析はHorse固有のビジネスルールです。ここをSDKに委ねる必要はありません。

推奨構造は次のとおりです。

```
ingestion/
├── router.go
├── extractor.go
├── verifier.go
└── provider/
    └── gemini/
        ├── client.go
        ├── schema.go
        └── errors.go
```

例えばプロバイダのインターフェースは、

```
type VisionExtractor interface {
    Extract(
        ctx context.Context,
        request ExtractRequest,
    ) (ExtractionResult, error)
}
```

のようにできます。

`google.golang.org/genai`を使った実装は`provider/gemini`内に閉じ込めます。

この設計であれば、将来GeminiのモデルやAPIが変わっても、会計処理やWorkerの実装を変更する必要がなくなります。

また、`receiptSchema()`は現在、`map[string]any`による手作りJSON Schemaです。これは`Receipt`構造体と二重管理になっています。

現時点でもフィールドが多く、今後の変更でスキーマとGoの構造体がずれるリスクがあります。

JSON Schema生成ライブラリの使用を検討し、Geminiが受け付けるSchemaの制約に合わせて変換するか、少なくともGoの型とSchemaの整合性を検証するContract Testを設けるべきです。

## 6. pgxのトランザクションAPIをもっと活用する

優先度：中〜高 / 保守性

`mutations.go`では、ほぼすべてのMutationが以下のパターンを使用しています。

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

ただし、pgxにはすでに`pgx.BeginFunc`と`pgx.BeginTxFunc`があり、コールバックの返り値に応じてCommit/Rollbackを管理できます。

[image](https://www.google.com/s2/favicons?domain=https://pkg.go.dev\&sz=32)

Go Packages

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

おすすめは、

Application Serviceがトランザクションを開始し、複数のRepository操作を同じTxで実行する

方式です。

例えば購入確定は、購入情報の保存、配賦、仕訳、EvidenceLink、イベントまで同じトランザクションで処理します。

一方、S3の原本保存やGemini APIへの呼び出しは、DBトランザクションの外側で実行します。

DBロックを保持したまま外部APIを待つ構成にはしないでください。

## 7. 読み取り専用APIを専用Query層に分離する

優先度：中〜高 / 設計・性能

現在はGraphQLのQuery Resolverに、データ検索と集計が直接実装されています。

具体的には以下です。

| 処理 | 現状の問題 | 推奨 |
| ------------------------- | ---------------------- | ------------ |
| `accountBalances` | 全仕訳をGoへ読み込んで集計 | Bob DSLによるDB集計 |
| `monthlyCategorySpending` | 購入ごとの品目検索 | Bob DSLによるDB集計 |
| `monthlyMemberSpending` | 購入・訂正ごとの繰り返し検索 | 集計専用Query |
| `productSuggestions` | 全商品の取得・Go側検索 | DB側絞り込み |
| `purchases` | `loadPurchase`を件数分実行 | バッチロード |
| `hasEvidence`フィルタ | 証拠リンクを全取得し巨大な`IN`条件を構築 | Bob DSLの存在条件 |

特に`hasEvidence`は、[queries.go](https://github.com/walnuts1018/horse/blob/2f3b983e6327e1781400175fd1f31893b373307c/internal/graph/queries.go#L178-L206)で、家庭内のEvidenceLinkとDocumentConfirmationを全部取得し、購入IDの集合をGoで構築しています。

これはBob DSLの相関存在条件で表現したほうが自然です。

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

## 8. OpenTelemetryとテスト基盤

### PostgreSQLのトレースを取得する

現在は`otelhttp`を使ったHTTP計測とOpenTelemetry SDKが導入されています。

一方、`cmd/api/main.go`や`cmd/worker/main.go`では`pgxpool.New`で直接DBプールを作成しており、pgxへのトレーシングを設定していません。

ここには otelpgx が適しています。

`otelpgx.NewTracer()`をpgxに設定すれば、SQLの実行時間やコネクション取得をTraceへ追加できます。DBプールの統計も収集可能です。

[image](https://www.google.com/s2/favicons?domain=https://pkg.go.dev\&sz=32)

Go Packages

これを導入しておくと、今回指摘したN+1や遅いSQLを実際のトレースから特定できます。

SQL全文やパラメータを不用意に収集しないよう、計測属性の設定には注意してください。

### テスト用Factoryを検討する

`bobgen.yaml`では、Factory生成も無効になっています。

```
factory:
  disabled: true
```

一方、`accounting_integration_test.go`などでは、大量の事前データ作成がテストコード内に記述されています。

BobにはFactory生成機能があるので、これを使ってテストデータ生成の重複を減らせる可能性があります。

[image](https://www.google.com/s2/favicons?domain=https://bob.stephenafamo.com\&sz=32)

Bob

ただし、会計テストでは正確な金額や負担関係が重要です。

Factoryであらゆる値を暗黙に補完するより、家庭・メンバー・プール・口座などの共通準備だけをFactoryに任せ、金額と精算条件はテスト内で明示する設計がよいです。

現状のCIではPostgreSQL 18を起動してDBマイグレーションと統合テストを実行しているので、テストのために別のデータベースやTestcontainersへ切り替える必要はありません。

## 9. 追加ライブラリなしで改善できるところ

すべてをライブラリで解決する必要はありません。

例えば、`/events`のSSEハンドラは、接続ごとに1秒間隔でPostgreSQLをポーリングしています。

複数ユーザーでの同時編集を前提にすると、将来的に無駄なDBアクセスが増える構造です。

ここはpgxの`LISTEN/NOTIFY`を使ってイベント到着を通知し、既存の`household_events`のカーソルで確実に取りこぼしを再取得する方式に変更できます。

新しくRedis Pub/SubやWebSocketライブラリを追加する必要はありません。

ただし、家庭内で数人が接続する程度なら現状のポーリングでも十分な可能性があります。クエリ数を計測してから改善してもよい部分です。

また、`log/slog`、`net/mail`、`mime/multipart`、`slices`など、現状で標準ライブラリを活用している部分はそのまま維持するのが適切です。

## ライブラリ採用の最終判断

| ライブラリ・機能 | 判断・理由 |
| ------------------------- | ------------------ |
| Bob ThenLoad / Preload | 積極活用 — N+1削減 |
| Bob Query Builder | 採用 — 型安全なDB検索・集計 |
| `google.golang.org/genai` | 採用 — 独自REST実装の削減 |
| `pgx.BeginFunc` | 積極活用 — Tx管理の簡素化 |
| `otelpgx` | 採用 — DBトレーシング |
| Bob Factory | 採用検討 — テストデータ生成 |
| gqlgen DataLoader | 積極的に活用。dataloader/v7を使う |
| 別のORM（GORM・Ent） | 不要 — Bobで十分 |
| 別のHTTPフレームワーク | echo/v5を用いる |
| DIコンテナ | kessokuを用いる |

## 実装する順番

| 順位 | 変更 | 効果 |
| -- | --------------------------------- | -------------- |
| 1 | Bobのバッチロードと型安全DSLを活用 | N+1解消、読み取り性能改善 |
| 2 | 購入・入金・精算などをApplication Serviceへ分離 | 保守性・再利用性 |
| 3 | 仕訳生成ロジックを純粋関数へ集約 | 重複削減、会計バグ防止 |
| 4 | Gemini通信を公式Go SDKへ移行 | API変更への追従性 |
| 5 | pgxのTransaction管理を統一 | 実装の簡潔さと一貫性 |
| 6 | otelpgxによるSQL計測 | 性能問題の発見 |
| 7 | テストデータ生成の共通化 | テストの読みやすさ |

### 最終的な評価

Horseのバックエンドで最も問題なのは、特定のライブラリが古いことでも、Goのコードが非効率なことでもありません。

現在はGraphQL Resolverがアプリケーションの本体になりすぎています。

この状態から、GraphQLを薄い入出力層にし、Application Serviceに業務処理を集約し、Bob/pgxを活用したDBアクセス層を明確に分けるだけで、かなり改善すると思います。

特に今回のアプリでは、同じ購入や精算ロジックが手入力、レシートAI取り込み、カード明細取り込み、締め後訂正から呼ばれます。

そのため、業務ロジックの再利用性は単なるコードの綺麗さではなく、すべての入力経路で同一の会計ルールを保証するための重要な設計要件です。

以上は確認したコミットに対する静的レビューです。性能改善の実測やリファクタリング後の動作確認は未実施ですが、まず着手する価値が高いのは、Bobのバッチ取得、Service層への責務分離、仕訳生成ロジックの集約です。

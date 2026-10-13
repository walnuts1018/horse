# Horseのバックエンドを外部ライブラリ中心に再設計する方針

結論から言うと、Job基盤はRabbitMQを中心にする方針に賛成です。HorseにRiverを導入する必要はないと考えます。

また、依存パッケージを増やしてよいのであれば、現在のHorseではさらに多くの自前実装を外部ライブラリに置き換えられます。

今回確認したのは、[walnuts1018/horse](https://github.com/walnuts1018/horse) の2026年10月13日時点の`main`（commit `c499869`）です。

## 1. Job基盤：RabbitMQとRiverの比較

| 観点 | RabbitMQ | River |
| ------------------- | ---------------------- | ---------------- |
| ジョブ保存先 | RabbitMQ | PostgreSQL |
| 非同期タスクの配送 | 得意 | 得意 |
| ワーカーの並列実行 | Consumer / Prefetch | Worker pool |
| 永続化 | Durable / Quorum Queue | PostgreSQLテーブル |
| Retry / Dead Letter | RabbitMQの機能・ライブラリ | Riverの機能 |
| DB更新とJob登録の原子性 | Outboxなどが必要 | PostgreSQL内で直接実現 |
| PostgreSQLへのジョブ管理負荷 | 小さくできる | 発生する |
| Horseの既存環境との相性 | 良い（導入済み） | RabbitMQと役割が重複 |

RiverはPostgreSQLをバックエンドとして使うGoのJobライブラリです。DBトランザクション内でジョブ登録できるのが大きな強みですが、その代わりジョブ実行状況の管理にもPostgreSQLを使用します。

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

HorseはすでにRabbitMQを利用しており、レシート解析には数十秒から数分かかる外部API呼び出しやOCRが含まれます。

この場合、ジョブ実行待ち・配送・再配信をRabbitMQに担当させ、PostgreSQLは家計データや解析結果の保存に専念させるのが自然です。

ただし、重要な区別があります。

RabbitMQをJob配送の正本にすることと、PostgreSQLからジョブ情報を完全になくすことは別です。

RabbitMQが持つべきものは「何を次に実行するか」で、PostgreSQLが持つべきものは「このレシートにどんな解析結果・確認履歴があるか」です。

現在のHorseはPostgreSQLにジョブを保存し、RabbitMQを単なる通知として利用しています。`worker.go`にはジョブのClaim・期限切れ回復・再試行スケジュールが実装され、RabbitMQが正常でも10秒間隔でDBを走査しています。

この二重管理はなくせます。

推奨する責務分離

RabbitMQ

ジョブ配送・待ち行列・再試行・Dead Letter・同時実行数の制御

PostgreSQL

原本メタデータ・解析バージョン・結果・費用・確認状態・監査証跡

ただし、PostgreSQLの保存とRabbitMQへのPublishを同時に確実に成功させるために、Transactional Outboxは残す必要があります。この点は後述します。

## 2. RabbitMQ向けGoライブラリは何を選ぶべきか

候補を比較すると、HorseにはWatermill + RabbitMQが最も合っています。

| ライブラリ | 特徴 | Horseでの判断 |
| -------------------------------------------------------------------- | ------------------------------------ | ------------ |
| [Watermill](https://github.com/ThreeDotsLabs/watermill) | メッセージ処理、Middleware、Retry、監視、Outbox連携 | 第一候補 |
| [watermill-amqp v3](https://github.com/ThreeDotsLabs/watermill-amqp) | RabbitMQ向けPublisher / Subscriber | Watermillと併用 |
| [Machinery v2](https://github.com/RichardKnop/machinery) | RabbitMQを利用できるTask Queueフレームワーク | 第二候補 |
| [amqp091-go](https://github.com/rabbitmq/amqp091-go) | RabbitMQの低レベルGoクライアント | 直接の利用を減らす |
| [River](https://github.com/riverqueue/river) | PostgreSQLベースのジョブ管理 | 今回は採用しない |

### Watermillを選ぶ理由

Watermillは、RabbitMQを含む複数のメッセージブローカーを抽象化し、メッセージハンドラーに共通のMiddlewareを組み込めます。

例えば、すでに次の機能が用意されています。

| 機能                              | Watermillでの対応                 |
| ------------------------------- | ----------------------------- |
| エラー時の短時間Retry                   | `middleware.Retry`            |
| Panicからの復帰                      | `middleware.Recoverer`        |
| 失敗メッセージの隔離                      | `middleware.PoisonQueue`      |
| 処理時間の制限                         | `middleware.Timeout`          |
| 処理数の制限                          | `middleware.Throttle`         |
| Correlation ID                  | `middleware.CorrelationID`    |
| Prometheusメトリクス                 | `components/metrics`          |
| PostgreSQL OutboxからRabbitMQへの転送 | `watermill-sql` + `Forwarder` |

これらは公式に提供されている機能です。

[image](https://www.google.com/s2/favicons?domain=https://watermill.io\&sz=32)

Event-Driven in Go

+2

現在の`cmd/worker/main.go`にある接続・Consume・Ack/Nack・再接続ループをかなり削減できます。

ただし、Watermillは汎用メッセージ処理フレームワークであって、Riverのような完全なJob管理システムではない点には注意してください。

長時間のバックオフ、失敗回数の永続管理、ジョブの優先順位、管理画面向けのステータスなどは、RabbitMQと組み合わせて設計する必要があります。

Machinery v2は、よりTask QueueらしいAPI、タスク登録、リトライなどを持っています。RabbitMQをBrokerとして使えるので、こちらも十分候補になります。

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

それでもHorseでWatermillを第一候補にするのは、今後レシート抽出だけでなく、注文取り込み、突合、商品補完、家計イベントなどの非同期処理も統一して扱いやすいためです。

### 注意：Watermillにも確認すべき互換性がある

Watermill AMQP v3でQuorum Queueを利用する場合、次の項目は実際のRabbitMQに対して統合テストを実施してください。

特に`Publish.ConfirmDelivery`は明示的に有効化し、確実にRabbitMQへ届いたことを確認します。また、RabbitMQが再配送時に追加する`x-delivery-count`などの数値型ヘッダーを、WatermillのデフォルトMarshalerが適切に処理できるか確認が必要です。

[image](https://www.google.com/s2/favicons?domain=https://pkg.go.dev\&sz=32)

Go Packages

+1

デフォルトMarshalerが数値ヘッダーを処理できない構成では、`PreprocessDelivery`でヘッダーを変換・除外するか、カスタムMarshalerを使います。

ここは単に「Watermillを使えば安全」とはせず、RabbitMQ 4.3のQuorum Queueで再配送・接続断・Dead Letterまでテストすることを推奨します。

## 3. 推奨するJobアーキテクチャ

Go API — ファイル受信・DB更新

PostgreSQL

原本情報 + Outbox

SeaweedFS

レシート原本

Watermill Forwarder / Publisher Confirm

RabbitMQ Quorum Queues

Extraction

Reconcile

Retry / DLQ

Go Worker — Watermill Router

Gemini

PP-OCR

Go検証

PostgreSQL — 解析結果・費用・履歴を保存

### 3.1 Transactional Outboxだけは残す

現在のHorseは、DBへ解析Jobを登録してCommitしたあとに、RabbitMQへ通知しています。

この方式ではRabbitMQへのPublishが失敗してもDBのレコードが残るため、DB Pollingによって回復できます。

RabbitMQをJob配送の正本に変更する場合、これを単純に削除すると、次の問題が発生します。

1. APIが原本をDBに登録しCommitする。
2. RabbitMQへのPublish直前にAPI Podが停止する。
3. 原本は登録済みだが、ジョブはRabbitMQに存在しない。

したがって、Transactional Outboxは維持する必要があります。

ここでは [Watermill Forwarder](https://watermill.io/advanced/forwarder/) と [watermill-sql](https://watermill.io/pubsubs/sql/) の利用を推奨します。

[image](https://www.google.com/s2/favicons?domain=https://watermill.io\&sz=32)

Event-Driven in Go

+1

DBトランザクション内で原本と小さなOutboxイベントだけを保存し、ForwarderがRabbitMQへの転送成功を確認したらOutboxから削除または送信済みにします。

送信成功後・Outbox更新前に停止した場合は重複配送され得るため、Worker側に冪等性が必要です。

これはPostgreSQLをJob Queueとして使っているのとは違います。

PostgreSQLが担当するのはDB更新とイベント発行を原子的に結び付けることだけです。長時間のJob待ち・Retry・Consumerへの配送はRabbitMQが担当します。

なお、Horseの処理量ではOutboxのDB負荷もRiverのJob管理負荷も絶対量としては小さいと考えられます。RabbitMQ移行の理由は「PostgreSQLの負荷が必ず大幅に下がる」よりも、責務の明確化と既存RabbitMQの有効活用にあります。

### 3.2 RabbitMQのRetry機能を利用する

ここはRabbitMQのバージョンが重要です。

RabbitMQ 4.3以降では、Quorum QueueにネイティブのDelayed Retry機能が追加されています。 再配信を一定時間遅らせ、再配信回数に応じて遅延を増加させられます。

[image](https://www.google.com/s2/favicons?domain=https://www.rabbitmq.com\&sz=32)

RabbitMQ

+1

これを積極的に活用すれば、自前の`retryDelay()`やジョブ復旧Pollerの一部を削除できます。

ただし、AMQP 0.9.1の`basic.nack`による再投入では、RabbitMQ 4.3の配送失敗回数が増えない場合があります。そのため、配送上限とRetryの動作を`basic.reject`などの挙動も含めて検証する必要があります。

[image](https://www.google.com/s2/favicons?domain=https://www.rabbitmq.com\&sz=32)

RabbitMQ

また、Geminiの無料枠を翌日まで待つケースでは、数分単位のRetryでは不十分です。その場合はTTL付きRetry QueueとDead Letter Exchangeを組み合わせるか、別の遅延配送の仕組みを使います。

おすすめの分類は次のとおりです。

| エラー | 処理 |
| ----------------- | --------------------- |
| ネットワーク一時障害 | RabbitMQ側で遅延Retry |
| Gemini 429（短時間制限） | `Retry-After`を考慮して再配送 |
| Gemini無料枠の日次上限 | 翌日の無料枠回復に合わせて遅延配送 |
| Gemini有料予算上限 | 長時間延期。必要に応じて手動再開 |
| 不正な文書・非対応形式 | RetryせずDLQ |
| JSONが不正 | 別モデルへの再解析または限定回数Retry |
| 恒久的な失敗 | DLQとDB上の失敗状態を記録 |

長時間RetryはWatermillのHandler内で`time.Sleep`するのではなく、RabbitMQにいったんメッセージを戻してConsumerを解放する方がよいです。

### 3.3 長時間ジョブとAck Timeout

ここも注意が必要です。

現在のHorseではWorkerの処理Contextに32分の上限があります。一方、RabbitMQのConsumer Ack Timeoutのデフォルトは30分です。

RabbitMQはAck Timeoutを超えると、ConsumerのChannelを閉じてメッセージを再配送する可能性があります。

[image](https://www.google.com/s2/favicons?domain=https://www.rabbitmq.com\&sz=32)

RabbitMQ

RabbitMQ 4.3のQuorum Queueでは、このTimeoutをQueue単位で設定できます。

そのため、Horseでは例えば60分以上のAck Timeoutを設定し、ジョブの最大実行時間より余裕を持たせます。

また、`prefetch=1`から始め、並列度はWorker Podの数または明示的なWorker数で制御します。

Geminiを実行中にWorkerが停止して再配送された場合、API課金が二重になる可能性は残ります。保存済みの成功結果を検査して不要な再処理を避けることはできますが、外部API呼び出しを含めて厳密なExactly Onceを保証することはできません。

## 4. Job基盤以外で積極的に採用したいライブラリ

ここからが保守性の改善として、かなり効果の大きい部分です。

| ライブラリ | 置き換え対象・メリット |
| -------------------------------------------------------------------------------- | --------------------------------------- |
| [Bob Factory](https://bob.stephenafamo.com/docs/code-generation/factories/) | 統合テストのDBレコード生成、Fixtureの簡素化 |
| [Bob Query Generation](https://bob.stephenafamo.com/docs/code-generation/intro/) | 複雑なSQLの型付きGoコード生成 |
| [caarlos0/env](https://github.com/caarlos0/env) | 環境変数の読み取り・デフォルト値・型変換・必須チェック |
| [validator/v10](https://github.com/go-playground/validator) | API入力と設定値の構造的な検証 |
| [go-message](https://github.com/emersion/go-message) | RFC対応のメールヘッダー、MIME、添付ファイル、文字コード解析 |
| [html-to-markdown/v2](https://github.com/JohannesKaufmann/html-to-markdown) | HTML注文メールを構造を保ったMarkdownへ変換 |
| [mimetype](https://github.com/gabriel-vasile/mimetype) | アップロードファイルの内容に基づくMIME判定 |
| [chi/v5](https://github.com/go-chi/chi) | RESTルートのグループ化、認証Middleware、共通Middleware |
| [testify](https://github.com/stretchr/testify) | テストアサーション、失敗時の差分表示、テスト構造の改善 |
| [testcontainers-go](https://golang.testcontainers.org/) | PostgreSQL / RabbitMQを使った自動統合テスト |

### 特におすすめの3つ

Bob Factory + Query Generation

現在の`bobgen.yaml`では両方無効になっています。これらを有効にすれば、テスト用の複雑な家庭・口座・購入データ生成と、手書きSQLの型付きアクセスコードの作成を生成器に任せられます。

すでにBobに統一している以上、最初に着手する価値があります。

[image](https://www.google.com/s2/favicons?domain=https://bob.stephenafamo.com\&sz=32)

Bob

+1

caarlos0/env

現在の`cmd/worker/main.go`には、環境変数の取得、パース、エラーハンドリング、デフォルト値の設定が長く書かれています。

これを次のような構造体へ置き換えられます。

```
type WorkerConfig struct {
    DatabaseURL string `env:"DATABASE_URL,notEmpty"`
    RabbitMQURL string `env:"RABBITMQ_URL,notEmpty"`

    GeminiEnabled bool `env:"GEMINI_ENABLED" envDefault:"false"`
    FreeAPIKey     string `env:"GEMINI_FREE_API_KEY"`
    PaidAPIKey     string `env:"GEMINI_PAID_API_KEY"`

    USDJPYRate float64 `env:"GEMINI_USDJPY_RATE" envDefault:"158.535"`
}

cfg, err := env.ParseAs[WorkerConfig]()
```

さらに`validator`で、Geminiを有効にしたときはAPIキーを最低1つ必要とする、といったフィールド間の検証を実施できます。

[image](https://www.google.com/s2/favicons?domain=https://pkg.go.dev\&sz=32)

Go Packages

`envDefault`の為替レートは上のコードでは現行値に合わせた例です。実際の費用計測では取得日時とレートの出典も保持し、永続的に固定しないようにします。

go-message + html-to-markdown

現在の`internal/ingestion/email.go`は、自前でMIMEツリーを辿り、転送エンコーディングをデコードし、HTMLからテキストを作成しています。

これは外部ライブラリに最も置き換えやすい部分の1つです。

`go-message`にはMIMEと各種文字コードの読み取り機能があり、`html-to-markdown`は表やリストなどを残したテキスト化ができます。

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

+1

Amazonなどの注文メールではHTMLテーブルの構造が重要なので、単純にタグを削除するより有利です。

もちろん、注文番号や金額の意味解釈はアプリ固有の仕事なので残します。

## 5. さらに改善するなら

### GraphQL周辺

gqlgenはそのまま使い、`nodes(ids:)`や購入・商品・口座の関連データ取得にはバッチロードを導入します。

gqlgenが紹介しているDataLoader方式を採用し、既存のBobによるリレーションロードと使い分ける方針です。

GraphQLの入力検証も、共通の構造体検証へ寄せられる部分は寄せます。ただし金額整合性・負担割合・締め済み期間の判定は、単純なタグベースのValidatorでは代替できません。

### HTTP API周辺

現在は標準の`http.ServeMux`でAPIを組み立てています。

これも十分な機能を持ちますが、`chi/v5`なら認証済みAPIと公開APIのグルーピング、共通Middlewareの適用、ルート構造の明確化が容易になります。

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

例えばDocument APIを認証済みルーターにまとめることで、各HTTP Handler内で同じ認証処理を呼ぶコードを減らせます。

ただし、認証Middlewareが認可まで自動的に保証するわけではありません。 家庭ごとのリソース所属確認はApplication側で維持します。

### テスト環境

PostgreSQL 18を使用したCI統合テストはすでに存在するので、これは維持します。

加えて`testcontainers-go/modules/postgres`と`testcontainers-go/modules/rabbitmq`を利用すれば、開発環境でも同じ系統の統合テストを自動的に起動できます。

[image](https://www.google.com/s2/favicons?domain=https://golang.testcontainers.org\&sz=32)

Testcontainers for Go

+1

特にRabbitMQのジョブ移行では、単体テストだけでは不十分です。Consumerの強制終了、Publisher Confirm直前の停止、再配送、DLQ、長時間ジョブ、重複処理の試験が必要です。

## 6. 目指すべき最終構成

私なら、Horseのバックエンドを次のように整理します。

```
internal/
├── api/
│   ├── server.go
│   ├── documents.go
│   └── middleware/
│
├── graph/
│   ├── *.resolvers.go
│   └── model/
│
├── application/
│   ├── purchase/
│   ├── accounting/
│   ├── settlement/
│   ├── funding/
│   ├── transfer/
│   └── catalog/
│
├── domain/
│   ├── allocation/
│   ├── money/
│   └── settlement/
│
├── postgres/
│   ├── generated/
│   └── queries/
│
├── messaging/
│   ├── rabbitmq.go
│   ├── outbox.go
│   └── handlers/
│
├── ingestion/
│   ├── extraction.go
│   ├── gemini.go
│   ├── receipt.go
│   ├── email.go
│   └── reconciliation.go
│
├── config/
├── auth/
└── observability/
```

設計上の境界は次のようにします。

| レイヤー | 使用する主なライブラリ | 自前で維持するもの |
| ----------- | -------------------------------------- | -------------------------- |
| GraphQL | gqlgen + DataLoader | GraphQL入力とApplicationの対応付け |
| HTTP | chi + net/http | API固有の処理 |
| Application | Bob + pgx | 購入・訂正・精算などの業務操作 |
| Domain | 必要に応じてDecimal等 | 会計の不変条件と負担計算 |
| DB | Bob Factory / Query Gen + Atlas | アプリ固有のSQL |
| Messaging | Watermill + AMQP + SQL Forwarder | ジョブの意味と結果の永続化 |
| AI | google.golang.org/genai | 抽出スキーマと検証・モデル選択 |
| メール | go-message + html-to-markdown | 注文情報の抽出 |
| Config | caarlos0/env + validator | 設定間の整合性 |
| Test | testify + Testcontainers + Bob Factory | 業務シナリオと期待結果 |

ポイントは、外部ライブラリに任せる範囲を広くする一方で、家計簿固有の会計ロジックだけは明確なドメインコードとして残すことです。

会計上の不変条件まで汎用ライブラリに置き換えようとすると、むしろ重要なルールが隠れてしまいます。

## 7. 実装する順番

1. RabbitMQジョブ基盤の再構成 — Watermillを導入し、PostgreSQLのジョブClaim・リース・再試行管理を廃止。Transactional Outboxだけ残す。
2. 購入・会計処理の統一 — `application/purchase`への移行を完成させ、会計仕訳と負担スナップショットの更新処理を一元化。
3. Bobの活用強化 — Factory・Query Generationを有効にし、精算処理のN+1を解消。
4. メール・設定・HTTPの自前コード削減 — go-message、html-to-markdown、caarlos0/env、validator、chiを導入。
5. テスト基盤を共通化 — Bob FactoryとTestcontainersで、複雑なセットアップコードを削減し、RabbitMQの障害試験を追加。

### 最終的なおすすめ

RabbitMQは維持し、Riverは導入しない。RabbitMQ向けの処理基盤はWatermillを第一候補にする。

PostgreSQLは業務データの保存とTransactional Outboxに集中させ、Jobの配送・再試行・DLQはRabbitMQへ移す。

Bob、gqlgen、pgxは入れ替えず、Factory・Query Generation・DataLoaderなどの周辺機能を積極的に活用する。

メール、設定、HTTP、テストについては新しいライブラリを導入して自前実装を減らす。

特にHorseはまだ未リリースなので、これらの変更を今行うメリットが大きいです。

一つだけ優先順位を強調すると、会計処理の一本化はJob基盤の置き換えより先に安全性を確保すべき領域です。RabbitMQへの移行と独立して回帰テストを固めておけば、その後はライブラリを積極導入しても安心してコードを整理できます。

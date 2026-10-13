# Horseのバックエンドを外部ライブラリ中心に再設計する方針

依存パッケージを増やしてよいので、現在のHorseではさらに多くの自前実装を外部ライブラリに置き換えられます。

## 1. 積極的に採用したいライブラリ

ここからが保守性の改善として、かなり効果の大きい部分です。

| ライブラリ | 置き換え対象・メリット |
| -------------------------------------------------------------------------------- | --------------------------------------- |
| [Bob Factory](https://bob.stephenafamo.com/docs/code-generation/factories/) | 統合テストのDBレコード生成、Fixtureの簡素化 |
| [Bob Query Generation](https://bob.stephenafamo.com/docs/code-generation/intro/) | 複雑なSQLの型付きGoコード生成 |
| [validator/v10](https://github.com/go-playground/validator) | API入力の構造的な検証 |
| [go-message](https://github.com/emersion/go-message) | RFC対応のメールヘッダー、MIME、添付ファイル、文字コード解析 |
| [html-to-markdown/v2](https://github.com/JohannesKaufmann/html-to-markdown) | HTML注文メールを構造を保ったMarkdownへ変換 |
| [mimetype](https://github.com/gabriel-vasile/mimetype) | アップロードファイルの内容に基づくMIME判定 |
| [chi/v5](https://github.com/go-chi/chi) | RESTルートのグループ化、認証Middleware、共通Middleware |
| [testify](https://github.com/stretchr/testify) | テストアサーション、失敗時の差分表示、テスト構造の改善 |
| [testcontainers-go](https://golang.testcontainers.org/) | PostgreSQL / RabbitMQを使った自動統合テスト |

### 特におすすめの項目

Bob Factory + Query Generation

現在の`bobgen.yaml`では両方無効になっています。これらを有効にすれば、テスト用の複雑な家庭・口座・購入データ生成と、手書きSQLの型付きアクセスコードの作成を生成器に任せられます。

すでにBobに統一している以上、最初に着手する価値があります。

[image](https://www.google.com/s2/favicons?domain=https://bob.stephenafamo.com\&sz=32)

Bob

+1

go-message + html-to-markdown

現在の`internal/ingestion/email.go`は、自前でMIMEツリーを辿り、転送エンコーディングをデコードし、HTMLからテキストを作成しています。

これは外部ライブラリに最も置き換えやすい部分の1つです。

`go-message`にはMIMEと各種文字コードの読み取り機能があり、`html-to-markdown`は表やリストなどを残したテキスト化ができます。

[image](https://www.google.com/s2/favicons?domain=https://github.com\&sz=32)

GitHub

+1

Amazonなどの注文メールではHTMLテーブルの構造が重要なので、単純にタグを削除するより有利です。

もちろん、注文番号や金額の意味解釈はアプリ固有の仕事なので残します。

## 2. さらに改善するなら

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

## 3. 目指すべき最終構成

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
| AI | google.golang.org/genai | 抽出スキーマと検証・モデル選択 |
| メール | go-message + html-to-markdown | 注文情報の抽出 |
| Config | caarlos0/env + validator | 設定間の整合性 |
| Test | testify + Testcontainers + Bob Factory | 業務シナリオと期待結果 |

ポイントは、外部ライブラリに任せる範囲を広くする一方で、家計簿固有の会計ロジックだけは明確なドメインコードとして残すことです。

会計上の不変条件まで汎用ライブラリに置き換えようとすると、むしろ重要なルールが隠れてしまいます。

## 4. 実装する順番

1. 購入・会計処理の統一 — `application/purchase`への移行を完成させ、会計仕訳と負担スナップショットの更新処理を一元化。
2. Bobの活用強化 — Factory・Query Generationを有効にし、精算処理のN+1を解消。
3. メール・HTTPの自前コード削減 — go-message、html-to-markdown、chiを導入。
4. テスト基盤を共通化 — Bob FactoryとTestcontainersで、複雑なセットアップコードを削減。

### 最終的なおすすめ

Bob、gqlgen、pgxは入れ替えず、Factory・Query Generation・DataLoaderなどの周辺機能を積極的に活用する。

メール、HTTP、テストについては新しいライブラリを導入して自前実装を減らす。

特にHorseはまだ未リリースなので、これらの変更を今行うメリットが大きいです。

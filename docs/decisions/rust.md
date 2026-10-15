# Horse を DMMF に忠実に Rust で再設計する方針

GraphQL Schemaはそのままに、BEをRustに移行する。ついでにDBリセットを前提としてDB Schemaもこの機会に完璧で洗練されたものにする。
DMMFについてわからないことがあれば必ずユーザーに問い合わせる。積極的に問い合わせる。

Rustで全Backend機能と公開契約を実装し、必要な検証を完了してから、Go APIとWorkerを一括でRustへ切り替える。GoとRustの機能単位の共存移行は完了条件にしない。

また、本番環境はまだリリース前なので、破壊的な切り替えを行なってOK。ダウンタイムもデータロストも一切問題ありません。

## 1. 結論：Clean Architecture の Rust 版ではなく、Functional Domain Modeling を中心に設計する

私なら、Horse の Rust 再実装は次の原則で設計します。

「Bounded Context ごとに独立したドメインモデルを持ち、業務操作を型付き Workflow として表現し、純粋関数による判断と、外側で実行する副作用を明確に分離する」

採用する全体像は、以下です。

GraphQL / HTTP / Worker

外部契約、認証、DTO、入力変換

Command / Query

Bounded Context ごとの Workflow

Validate

Decide

Produce

型付き状態遷移・純粋関数・Result による合成

Pure Domain Core

Value Objects / Aggregates / Domain Functions

I/O なし・SQL なし・GraphQL なし

明示的な I/O 境界

Infrastructure / Effect Interpreters

PostgreSQL・Object Storage・AI・OIDC・Outbox・Messaging

この構成にはいくつか重要な特徴があります。

- BC を第一の分割単位にする。 `domain/`、`application/`、`infrastructure/` という全アプリ共通の横方向の分割を主構造にしません。
- Workflow を設計の中心にする。 GraphQL Resolver や Service メソッドではなく、業務上意味のある Command と、その結果を中心に据えます。
- 関数による依存性注入を優先する。 何でも `trait Repository` に抽象化するのではなく、各 Workflow が必要とする能力を小さく渡します。
- Rust の型システムを積極的に使う。 `struct`、`enum`、newtype、Smart Constructor、`Result`、所有権を、不正状態と不正な遷移の抑止に使います。
- 技術的な依存と業務的な依存を区別する。 SQL がドメインを決めることも、GraphQL スキーマが集約構造を決めることも避けます。
- CQRS は論理的に採用し、Event Sourcing は原則として採用しない。 会計監査に必要な追記型履歴と、Event Sourcing を混同しません。

これは DMMF の第3章（関数型アーキテクチャ）、第5〜7章（型と Workflow）、第9〜10章（合成とエラー処理）、第11〜12章（境界と永続化）を、Rust で実現する方針です。

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

ただし、DMMF に厳密に従うことと、F# の実装形式を一字一句 Rust に模倣することは違います。 書籍が要求する性質を保ちながら、Rust の所有権や非同期処理に適した構造へ翻訳するべきです。

## 2. 既存 Horse の何を維持し、何を作り直すか

GitHub の `main` ブランチを確認したところ、Horse は単純な家計簿 CRUD ではありません。

既存コードには、家庭単位のマルチテナンシー、購入・品目・負担配賦、複数決済、複式仕訳、精算、月次締め、訂正、貸付・返済、証拠文書、AI 抽出、複数ソース照合、非同期 Worker、共同編集が含まれています。

技術構成は Go/gqlgen/Bob/PostgreSQL を中心に、ZITADEL、SeaweedFS、RabbitMQ、Gemini、OCR サービスを接続したものです。詳しくは [README](https://github.com/walnuts1018/horse/blob/main/README.md) と [設計要件](https://github.com/walnuts1018/horse/blob/main/docs/plan.md) に記載されています。

ここで私が区別したいのは、次の2つです。

維持するのは業務上の意味と、不変条件です。 例えば「商品の最終負担と実際の決済元は別」「負担割合を変更しても締め済みの過去を変更しない」「月次締め後の訂正には履歴を残す」「借金と支出を混同しない」という性質です。

作り直すのは、それらを実現する技術的なモデルです。 既存の package、Service、DB テーブル、GraphQL 型、Repository の構造、さらには現行の状態遷移の切り方まで、そのまま継承する必要はありません。

実際、既存 Go コードにはすでに純粋関数的な部分があります。`internal/domain/allocation.go` は最大剰余法による整数金額の配分を実装し、`internal/domain/settlement.go` は精算額を計算しています。一方で `internal/application/purchase/post.go` では、状態確認、行ロック、DB 操作、仕訳作成、イベント保存が一つの処理に連続して登場します。

したがって、現在ある純粋な計算の考え方を維持しつつ、業務判断と I/O の境界をさらに徹底して分離するのが、今回の再設計で最も効果の大きい方向です。

## 3. Bounded Context は「技術機能」ではなく「業務上の整合性」で分割する

DMMF 第1章 §1.3–1.5、第3章 §3.1–3.4 では、サブドメインと Bounded Context を区別し、BC をサービスのデプロイ単位と同一視しないことが重要です。

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

Horse では、既存の Go package の名前から機械的に BC を決めるべきではありません。

私の初期案は次の構成です。

Household

家庭・メンバー・招待・所有権・役割

Catalog & Policy

商品・カテゴリ・既定負担ルール・商品別ルール

Bookkeeping（中核 BC）

この中は別 BC にせず、ドメインモジュールとして分割

Purchase / Correction

Allocation / Payments

Journal / Transfers

Funding / Settlement / Period

Evidence & Ingestion

原本・抽出ジョブ・OCR/AI・観測・照合候補・Provenance

公開契約・イベント・読み取りモデル

Reporting / GraphQL Queries

集計・残高・ホーム・分析用 Read Model

これは最終確定の Context Map ではありませんが、今回の前提なら有力です。

### 3.1 Bookkeeping を大きめの BC にする理由

最も重要な判断は、Purchase、Journal、Settlement を最初から別 BC にしないことです。

Horse では次の操作が強く関係します。

| 業務操作 | 必要な整合性 |
| -------- | --------------------------- |
| 購入の確定 | 購入状態、決済、負担スナップショット、仕訳が矛盾しない |
| 購入の訂正 | 元履歴を保持しながら訂正仕訳を追加する |
| 月次締め | 精算結果を固定し、対象月の追加記帳や不正な変更を防ぐ |
| 入金・送金・返済 | 資金移動と仕訳、貸付残高の整合性を維持する |
| 明細の確定 | 同一取引を重複記帳させず、証拠との対応を記録する |

既存の `post.go`、`correction.go`、`settlement/service.go` にも、複数レコードを同一トランザクションで扱う仕組みがあります。

これらを最初から別 BC として非同期メッセージで連携すると、購入は確定しているのに仕訳がない状態や、締め処理との競合などを新たに解決する必要が出ます。

DMMF が推奨するのは BC の数を増やすことではありません。モデルの一貫性を持つ境界を発見することです。

そのため、Bookkeeping 内に複数の Aggregate と Workflow を設け、PostgreSQL の単一トランザクションで必要な整合性を保つ設計を推奨します。

### 3.2 Catalog & Policy は別の言語を持たせる

Catalog の `ProductVariant` は、「どの商品を意味しているか」を表すものです。

Bookkeeping の `PurchaseItem` は、「その時点で実際にいくらの支出が発生し、誰が負担したか」を表すものです。

同じ商品を参照していても意味は異なります。

そこで Catalog 側から、例えば `ResolvedAllocationPolicy` とそのバージョンを公開契約で受け取り、Bookkeeping 側で確定時の負担配分をスナップショット化します。

後日、Catalog で商品名やデフォルト負担率が変わっても、確定済みの購入に影響しない構造です。

これは DMMF 第13章 §13.3.6 の、下流が必要とする意味だけを公開する原則にも対応します。

### 3.3 Evidence & Ingestion は「事実」と「推定」を区別する

これは Horse 固有の重要なドメイン境界です。

AI がレシートから「牛乳 250円」と推定したことは、ユーザーが牛乳を250円で購入したと確定したこととは違います。

したがって、以下を別概念にします。

- `SourceDocument`：取り込んだ原本
- `ExtractedObservation`：原本から抽出された候補
- `ReconciliationProposal`：既存取引との一致候補
- `ConfirmedSourceReference`：人間または明示的な業務ルールによって確定した対応
- `Purchase`：Bookkeeping の正規の取引

特に、`ExtractedObservation` を `Purchase` と同一視してはいけません。

確定済みソース参照の一意性と購入の記帳は、Bookkeeping 側が所有する確定記録として同一トランザクションで守ります。Ingestion 側は原本・抽出結果の正本を所有し、確定後の表示状態をイベント等で反映します。これなら原本・推定結果のモデルと、会計上の事実を分けながら、二重記帳を防げます。

### 3.4 デプロイは Modular Monolith にする

この4つの BC を、最初から4つのマイクロサービスにはしません。

1つの Rust workspace に配置し、API と Worker を別バイナリとして構成します。PostgreSQL は共用でき、BC ごとにスキーマや所有テーブルを分離します。

BC 間の直接的なドメイン型共有を避ければ、実行プロセスを分けずにモデルの独立性を得られます。

また Reporting は最初は独立したビジネス BC とせず、複数 BC の公開データから読み取りモデルを構築する側とします。業務判断の責務が育ってきたときにだけ、新しい BC として再評価します。

## 4. Rust workspace とレイヤーの設計

DMMF に忠実にするなら、一般的な Clean Architecture のように、全機能を横断する `domain`、`usecase`、`repository`、`controller` の4層を作ることには慎重であるべきです。

私なら Bounded Context を縦に分割し、各 BC の内側で Pure Domain と Workflow と Adapter を分ける構成にします。

推奨する初期ディレクトリ構造は次のようになります。

```

horse/
├── Cargo.toml
├── apps/
│   ├── api/                   # GraphQL / HTTP
│   └── worker/                # 非同期ジョブ
│
├── crates/
│   ├── household/
│   │   ├── domain/
│   │   ├── workflows/
│   │   └── contracts/
│   │
│   ├── bookkeeping/
│   │   ├── domain/
│   │   │   ├── money.rs
│   │   │   ├── purchase.rs
│   │   │   ├── allocation.rs
│   │   │   ├── journal.rs
│   │   │   ├── funding.rs
│   │   │   ├── settlement.rs
│   │   │   └── period.rs
│   │   ├── workflows/
│   │   │   ├── post_purchase.rs
│   │   │   ├── correct_purchase.rs
│   │   │   ├── close_period.rs
│   │   │   └── record_transfer.rs
│   │   └── contracts/
│   │
│   ├── catalog/
│   │   ├── domain/
│   │   ├── workflows/
│   │   └── contracts/
│   │
│   ├── ingestion/
│   │   ├── domain/
│   │   ├── workflows/
│   │   └── contracts/
│   │
│   ├── application/           # I/Oを伴う実行制御
│   │   ├── bookkeeping/
│   │   ├── household/
│   │   └── ingestion/
│   │
│   ├── infrastructure/
│   │   ├── postgres/
│   │   ├── object_storage/
│   │   ├── messaging/
│   │   ├── ai/
│   │   └── identity/
│   │
│   └── graphql/
│       ├── mutations/
│       ├── queries/
│       ├── subscriptions/
│       └── dto/
│
├── migrations/
└── tests/

```

概念的な配置例です。各 context は実際には `src/` 以下に配置し、Cargo crate 間の依存を設定します。

### 4.1 Domain と Workflow の依存方向

基本ルールは以下です。

| 領域 | 依存してよいもの | 依存させないもの |
| -------------- | ------------------------ | --------------------- |
| Domain | 自分の BC の純粋な型・関数 | DB、HTTP、GraphQL、Tokio |
| Pure Workflow | 自分の Domain、Command、Event | SQL、外部 API 実装 |
| Application | Workflow、公開契約、必要な I/O 能力 | GraphQL 固有モデル |
| Infrastructure | DB や外部サービス、保存用 DTO | 他 BC の非公開 Domain |
| GraphQL | Application、公開 API 用 DTO | SQL・永続化モデル |

この表は本書にそのまま登場するレイヤー定義ではなく、DMMF の第3章 §3.5、第9章 §9.6、第12章 §12.1 からの Rust 向け設計判断です。

実際には `application` の I/O 実行部分と PostgreSQL Adapter が相互参照しないよう、能力の契約を Application 側に定義するか、特定の Workflow を実行する外側の Runner にまとめます。

また、Rust では crate を分けることで可視性境界をコンパイラーに強制できます。重要な Domain 型のフィールドを非公開にしたり、別 BC の内部モジュールを公開しなかったりすることに実質的な価値があります。

ただし、すべてのファイルを別 crate にする必要はありません。独立性を維持したい境界に crate を使い、細かな業務機能は module にする程度がよいと思います。

## 5. ドメイン型：DMMF の AND / OR モデリングを徹底する

これは Rust が非常に得意な部分です。

DMMF の第4〜6章で重要なのは、単純なデータ型をラップすること自体ではなく、実際の業務で存在し得ない状態を、型の値域から除外することです。

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

### 5.1 まず Value Object を定義する

例えば `Money` は Horse で非常に重要です。

```
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Yen(i64);

impl Yen {
    pub fn new(value: i64) -> Result<Self, AmountError> {
        if !(0..=1_000_000_000_000).contains(&value) {
            return Err(AmountError::OutOfRange);
        }

        Ok(Self(value))
    }

    pub fn value(self) -> i64 {
        self.0
    }
}
```

実際にはこのほかに、正の金額、符号付き調整額、配賦比率、有理数量、会計月などを区別します。

例えば：

| 型 | 意味 |
| ----------------------- | ------------------- |
| `Yen` | 0以上の有効な円金額 |
| `PositiveYen` | 0円を許さない支払・送金金額 |
| `SignedYenDelta` | 訂正や精算で使用する符号付き差分 |
| `AllocationWeight` | 正の配賦重み |
| `AllocationShares` | 重複がなく非空の配賦構成 |
| `RationalQuantity` | 分母が正で、正規化済みの有理数 |
| `AccountingPeriod` | 有効な会計年月 |
| `MerchantName` | Horse の店舗名制約を満たす文字列 |
| `PurchaseId` / `PoolId` | 意味の違う識別子 |

特に金額の配分は、既存 Horse の `domain.Allocate` が採用している最大剰余法を純粋関数として移すのが自然です。

中間演算には `i128` などを用いてオーバーフローを管理し、合計保存、非負性、端数処理の決定性を保証します。

Rust の `f64` を会計金額に使うべきではありません。

### 5.2 Struct と Enum の使い分け

例えば現在の Pool は、`kind: "PERSONAL"` や `"SHARED"` に依存しています。

これを業務上の違いとして表すなら、

```
pub enum Pool {
    Personal(PersonalPool),
    Shared(SharedPool),
}

pub struct PersonalPool {
    id: PoolId,
    owner: MemberId,
}

pub struct SharedPool {
    id: PoolId,
    funding_rule: FundingRule,
    settlement_policy: SettlementPolicy,
}
```

という設計が考えられます。

PersonalPool に owner が必須で、SharedPool に funding rule が必須なら、その違いを構造で保証できます。

`kind` が Personal なのに Owner がない、Shared なのに共有資金ルールがない、といった不正状態を減らせます。

ただし、実際の Household 設定フローで未構成の Pool が有効な状態として存在するなら、`UnconfiguredSharedPool` のような別状態が必要です。業務上有効な中間状態まで無理に排除してはいけません。

### 5.3 Smart Constructor は外部境界で必ず通す

DMMF 第6章 §6.1 の Smart Constructor を、そのまま Rust の非公開フィールドと `Result` に対応させます。

重要なのは、次の構築経路をすべて管理することです。

```

GraphQL Input ────────┐
                      │
Worker Message ───────┼─→ DTO / Raw Data
                      │        ↓
PostgreSQL Row ───────┘   Smart Constructors
                               ↓
                         Valid Domain Value

```

ここで、ドメイン型へ安易に `Deserialize` を derive しないことを推奨します。

Serde を使うのは基本的に外部 DTO 側です。

DB から読み込んだデータであっても、明示的な `TryFrom<StoredRow>` などでドメイン型へ再構築します。

DB の制約による保証を放棄するという意味ではありません。DB 制約とドメイン型は別々の層で不変条件を防御します。

## 6. Workflow の状態型をどう設計するか

ここは DMMF の中核です。

ただし Horse では、3種類の状態を区別する必要があります。

| 状態の種類 | Horse での例 | 表現方法 |
| -------- | ------------------- | ---------------- |
| 処理途中の状態 | 未検証 → 検証済み → 記帳準備済み | 段階ごとの struct |
| 永続的な業務状態 | Draft → Posted | enum / Aggregate |
| 長時間プロセス | 抽出待ち → 実行中 → 確認待ち | 永続化された状態機械 |

DMMF 第7章 §7.2–7.3、§7.9 に対応する区別です。

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

### 6.1 Purchase は段階ごとに違う型を使う

例えば、購入を確定する処理です。

UnvalidatedPurchase

入力値・未検証の参照

validate

ValidatedPurchase

値制約と参照整合性を検証

prepare posting

PostingPlan

決済・負担スナップショット・貸借一致を含む

commit transaction

Persisted PostedPurchase

Journal / Event / Outbox と原子的に保存

段階型の利点は、例えば `prepare_posting` が `UnvalidatedPurchase` を受け付けられなくなることです。

ただし、処理中の `ValidatedPurchase` が「未来永劫正しい」ことを意味するわけではありません。

所属、会計期間の Open 状態、参照される口座の存在、競合する更新などは、その時点の永続化状態に依存します。これらはトランザクション内で検証し、必要ならロックまたは楽観的同時実行制御で守ります。

### 6.2 永続状態の enum は独立した状態軸を混ぜない

例えば、

```
pub enum Purchase {
    Draft(DraftPurchase),
    Posted(PostedPurchase),
}
```

とし、締め状態は `AccountingPeriod` が管理します。

現行 GraphQL の `DRAFT`、`POSTED`、`CLOSED` をそのまま1つの `PurchaseStatus` enum に写すと、「購入が記帳されたか」と「記帳された会計月が締められたか」が混ざりやすくなります。

これは今回スキーマ互換性を捨てられる利点の一つです。

また、Post された購入の修正は、私なら原則として元の会計履歴を直接書き換えず、訂正・逆仕訳・再記帳という業務操作に統一します。

画面では単に「購入を編集」と見せても構いません。内部のモデルまで CRUD にする必要はありません。

この状態モデルを採用し、`Purchase`は`Draft`から`Posted`へだけ遷移し、`AccountingPeriod`は独立して`Open`から`Closed`へ遷移します。月次締めは購入revisionを変更せず、締め時点のrevisionと精算結果を保存します。締め済み購入の訂正は現在の開いた会計期間へ計上し、元の購入日時と訂正の有効会計期間を別々に記録します。既存GraphQLが購入状態として`CLOSED`を返すProjectionは、関連する`AccountingPeriod`から導出します。

## 7. Workflow は「Command → Decision → Events」を基本にする

私が最も採用したい実装パターンです。

DMMF の第3章 §3.4 と第12章 §12.1 は、Workflow や純粋な判断がイベントなどの結果を値として返し、副作用の実行を外側に分離する考え方を示しています。

Rust ではこれを、例えば次のような形にできます。

```
pub struct Decision<State, Event> {
    pub next: State,
    pub events: Vec<Event>,
}

pub fn decide_post_purchase(
    purchase: DraftPurchase,
    facts: PostingFacts,
) -> Result<
    Decision<PostedPurchase, BookkeepingEvent>,
    PostPurchaseError,
> {
    // 検証済みの情報から、記帳できるかを判断する
    // 負担スナップショットと仕訳を構築する
    // イベントを値として生成する
    // DB書き込みやイベント配送はしない
}
```

この例では、`PostingFacts` はトランザクション内で読み込んで検証した口座・期間・ルールなどの情報です。

`PostedPurchase` は、まだ DB に保存されたことを意味しません。保存に成功したという事実を表すのは、外側の Application から返す `PostPurchaseReceipt` などです。

これを分けると、Domain の関数は完全に純粋にできます。

### 7.1 外側の Application が実行するもの

実際の PostPurchase は概ね次の流れです。

```
// 構造を説明するための概念コード

async fn post_purchase(
    db: &PgPool,
    command: PostPurchaseCommand,
) -> Result<PostPurchaseReceipt, HandlerError> {
    let mut tx = db.begin().await?;

    // 1. 認可、重複実行、リビジョンを確認
    // 2. 必要な行をロックして状態を読み込む
    let facts = load_posting_facts(&mut tx, &command).await?;

    // 3. 純粋なドメイン判断
    let decision = decide_post_purchase(
        facts.draft,
        facts.posting,
    )?;

    // 4. 購入・仕訳・イベント・冪等性結果を保存
    persist_posting(&mut tx, &decision).await?;

    tx.commit().await?;

    Ok(to_receipt(decision))
}
```

実際の実装には、冪等性キーの予約と既存結果の再取得、競合時の処理、トランザクション中の認可保証、commit 結果不明時の再照会を加えます。

注目してほしいのは、Domain がトランザクションを開始しないことです。

Domain は、業務上何をすべきかを決めます。Application はそれを原子的に適用します。

### 7.2 Pure Core に入れるロジック

例えば `decide_post_purchase` が内部的に使う関数は、

```
validate_purchase
    ↓
resolve_allocations
    ↓
calculate_item_allocations
    ↓
validate_payment_total
    ↓
calculate_funding_snapshots
    ↓
build_balanced_journal
    ↓
create_posted_purchase
    ↓
create_events
```

のような関数群です。

必ずしもすべてを別関数にする必要はありませんが、各ステップに固有の意味と入出力型を与えます。

このパイプラインは、F# の `|>` を再現する独自マクロを作らなくても、Rust の通常の関数呼び出し、`?`、`map`、`and_then`、`map_err` で自然に表現できます。

### 7.3 すべての Workflow を強引に純粋関数1個にしない

ここは重要な留保です。

DMMF は I/O が途中に入る Workflow も想定しています。第12章 §12.1.1 は I/O と純粋ステップが交互に必要になる場合にも言及しています。

したがって、私なら次の2段階で考えます。

基本形：Functional Core / Imperative Shell

```
Load Facts (I/O)
      ↓
Decide (Pure)
      ↓
Commit Decision (I/O)
```

途中で照会が必要な場合：

```
Validate (Pure)
      ↓
Load Additional Facts (I/O)
      ↓
Decide (Pure)
      ↓
Commit (I/O)
```

複数の外部サービスが関係する複雑なケースを無理に1つの純粋関数に押し込むべきではありません。

また、すべての副作用を `Effect` enum に列挙する Free Monad 風の汎用実行器や、独自の Effect System を最初から実装することも勧めません。

それらは興味深い抽象化ですが、DMMF の核心ではありません。Horse では、通常の関数合成と明示的な Application Runner で十分に目的を達成できると考えます。

## 8. 依存性注入：Repository Trait より小さな関数を優先する

これは「できるだけ DMMF に従いたい」という要望に対して、特に意識的に選びたい部分です。

第9章 §9.5–9.6、第12章 §12.1.2 では、著者は大きな Repository 抽象化ではなく、必要な I/O 関数を渡すアプローチを示しています。

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

### 8.1 避けたい設計

```
trait PurchaseRepository {
    async fn find(...);
    async fn save(...);
    async fn delete(...);
    async fn list(...);
}

trait PurchaseService {
    async fn create(...);
    async fn update(...);
    async fn delete(...);
}
```

もちろん Repository が常に悪いわけではありません。

ただ、この形では「何ができるか」だけが抽象化され、「なぜその操作が必要なのか」が隠れやすくなります。

### 8.2 推奨する設計

純粋計算への依存注入なら、例えば関数そのものを受け取れます。

```
pub fn resolve_allocation<F>(
    item: &PurchaseItem,
    get_rule: F,
) -> Result<Allocation, AllocationError>
where
    F: Fn(&PurchaseItem) -> Option<AllocationRule>,
{
    // ...
}
```

ここで `get_rule` は単なる値の参照関数です。DB I/O を必要とするなら、先に Application 側でルールを読み込み、この関数へ渡します。

Rust では非同期の関数注入に `Fn`、`Future`、`Send`、ライフタイム、boxing などが絡みます。そのため、次のような実用上の基準を使います。

| ケース | 推奨 |
| ---------------------- | ----------------------- |
| 純粋なドメイン計算 | 普通の関数 |
| 純粋関数へのポリシー注入 | `Fn` / closure |
| ワークフロー単位の小さな外部能力 | 小さな関数または局所的 trait |
| トランザクションを共有する複数 SQL 操作 | 具体的な PostgreSQL Adapter |
| 大きく複雑な外部サービス | 狭い capability trait |
| 全ドメイン共通の汎用 Repository | 原則作らない |

例えば、`CheckMemberPermission`、`LoadPostingFacts`、`ReserveIdempotencyKey` はそれぞれ意味の違う操作です。

巨大な `IRepository` や `IUnitOfWork` にまとめるより、Workflow の外側にそれぞれの具体的な I/O を配置します。

ただし、PostgreSQL のトランザクションを跨ぐ関数を大量にクロージャ注入することまで徹底する必要はありません。ここでは特定の DB Adapter を使うほうが、寿命管理と整合性を明確にできます。

抽象化の基準はモックしやすさではなく、業務の意味と依存関係の明確さに置きます。

## 9. 永続化：PostgreSQL は継続、DB スキーマは再設計する

移行コストがゼロでも、私なら PostgreSQL は維持します。

Horse のように、会計、複数人の同時更新、期間締め、証拠確定、強い整合性が必要なシステムでは、PostgreSQL のトランザクション、制約、行ロック、集計能力が適しています。

ただし、現行の DB スキーマは全面的に再設計してよいと思います。

### 9.1 Write Model と Read Model を分ける

DMMF 第12章 §12.2 の CQS/CQRS に沿って、更新用と読み取り用を分離します。

Write Model は Aggregate の整合性、業務ルール、リビジョン、原子的な状態遷移を中心にします。

Read Model は、GraphQL が必要とする情報を効率的に取り出せる構造にします。

例えば、ホーム画面の残高や月別支出グラフを表示するために、毎回すべての `Purchase` Aggregate を読み込んでドメイン型に再構築する必要はありません。

SQL の集計や専用の projection を使用できます。

そして最初はどちらも PostgreSQL に置きます。物理 DB を2つにする必要はありません。

### 9.2 Aggregate の単位

Bookkeeping 内では、以下のような候補を考えます。

| Aggregate             | 主要な責務                   |
| --------------------- | ----------------------- |
| `Purchase`            | 品目と購入のライフサイクル、負担・決済の整合性 |
| `AccountingPeriod`    | Open / Closed、締め対象期間の管理 |
| `SettlementRun`       | 確定精算結果、繰越のスナップショット      |
| `Loan`                | 元本、返済、残高、過剰返済の拒否        |
| `FundingSchedule`     | 入金義務を生成する設定             |
| `SharedPurchaseDraft` | 共同編集中の状態・リビジョン・競合       |

これらを1つの巨大な Aggregate にしません。

しかし、例えば `Purchase` の確定に仕訳保存が不可欠なら、Aggregate 境界を越えても、同じ Bookkeeping BC 内で明示的なトランザクションを使えます。これは DMMF §6.5.3 にある例外と整合的な扱いです。

### 9.3 Journal は独立した純粋モデルにする

会計の正しさを支える重要な型として、

```
pub struct BalancedJournal {
    entries: NonEmptyVec<JournalEntry>,
}
```

を考えます。

この型は、すべての明細の金額が正しく、借方と貸方の合計が一致した場合だけ構築可能にします。

既存の `BuildEntries` と `BuildPurchaseEntries` の仕組みを、Rust ではより厳密な型の構築条件へ移せます。

DB 側でも必要な `CHECK`、`FOREIGN KEY`、一意制約、トランザクション終端での整合性検証を残します。

ドメイン型は SQL や並行トランザクションの正しさを代替しないためです。

### 9.4 同時更新と冪等性

Horse には既にリビジョンによる競合検出や冪等性キーがあります。これは維持すべき業務保証です。

推奨する原則は以下です。

- Aggregate の更新は `expected_revision` を確認する。
- 月次締めと同じ期間への記帳は、共通の会計期間ロックによって直列化する。
- 冪等性は `(household, operation, key)` と request fingerprint で管理する。
- 冪等性結果、購入、仕訳、公開予定イベントを同一トランザクションで確定する。
- commit の応答が不明な場合は、冪等性キーを使って確定結果を再取得する。

特に会計操作では、「純粋関数で計算したときの情報」と「実際に commit するときの情報」が異ならないことを保証する必要があります。

## 10. イベント設計：Domain Event と配送メッセージを分ける

DMMF 第3章 §3.4.2 は、BC 内部でイベントバスによって隠れた依存関係を作ることを警戒しています。

そこで私なら、同一 BC 内の処理は明示的な関数合成、BC 間の通知には公開イベントという方針を採用します。

例えば `PostPurchase` Workflow の結果は、

```
pub enum BookkeepingEvent {
    PurchasePosted(PurchasePosted),
    JournalRecorded(JournalRecorded),
}
```

のような内部ドメインイベントを返せます。

ただし、これらをそのまま JSON 化して BC 間へ配信するわけではありません。

公開側では、必要な情報だけを含む別の契約を作ります。

```
Bookkeeping Domain Event
          ↓
    Public Event Mapper
          ↓
     Versioned Event DTO
          ↓
       Outbox
          ↓
    Event Dispatcher
          ↓
  Consumer / Read Projection
```

イベント DTO には Event ID、Event Type、Schema Version、集約 ID、発生日時などを明示し、受信側では重複処理を防ぎます。

### 10.1 Transactional Outbox を採用する

DMMF 自体は完全な Outbox 実装を指示していません。しかし、Horse のような複数 BC を持つ実アプリでは有効です。

DB に購入だけ保存され、イベント送信に失敗した場合でも、Outbox に未配送イベントが残ります。

配信は少なくとも1回とし、Consumer 側を冪等にします。ネットワーク越しの厳密な exactly-once を前提にしません。

### 10.2 GraphQL Subscription は別の配信目的

既存の [purchase_subscription.resolvers.go](https://github.com/walnuts1018/horse/blob/main/internal/graph/purchase_subscription.resolvers.go) は、保存された Household Event をポーリングして変更を配信しています。

再設計では、業務イベントと UI 更新通知を区別します。

GraphQL Subscription は、クライアントに「再読込すべき変更がある」と知らせるための仕組みとして設計します。

BC 間のビジネス処理を GraphQL Subscription に依存させるべきではありません。

Rust実装では`purchaseChanges`と`sharedPurchaseDraftChanges`をこの用途に限定します。購入変更は共有ポーラーからbounded channelへ配信し、購読中もHouseholdの認可を定期的に確認します。購入変更はcursor、entity、revision、変更フィールドと関連するPurchaseを通知します。

HTTP APIは`async-graphql-axum`のWebSocket transportを`/graphql`のGETとして公開し、既存のPOSTによるGraphQL実行を維持します。接続時にOriginを`public_origin`または`frontend_origin`と照合し、`horse_session` cookieをDBで解決してからUpgradeします。未認証接続は拒否し、認証済み接続にはHTTP POSTと同じActorおよびViewerIdentityをGraphQL contextへ設定します。クライアントは`graphql-transport-ws`または`graphql-ws` protocolで接続できます。

購入品目の削除イベントには親Purchase IDを`household_events.payload`へ保存します。削除後は品目行から親IDを引けないためです。この変更履歴はGraphQLの更新通知に使い、BC間の公開イベントやTransactional Outboxの代わりにはしません。

## 11. AI 取り込み Worker は永続状態機械として設計する

DMMF 第7章 §7.9 の Saga / Process Manager に最も近い Horse の機能が、AI 文書取り込みです。

ここでは純粋な短時間 Workflow と、長時間にわたる実行状態を明確に分けます。

Queued

Leased

Extracted

失敗・タイムアウト・クォータ制限

RetryScheduled

期限到来で再試行

PermanentlyFailed

明示的な最終失敗

抽出が成功しても会計への確定は別 Workflow

Human Review → Confirm Observation

このモデルでは、Job の claim、lease token、試行回数、次回実行時刻、失敗種別などはすべて明示的な状態です。

既存 Horse の `ingestion/worker.go` や `worker_results.go` は、attempt を利用して遅延 Worker の書き込みを防いでいます。この考え方は引き継ぐべきです。

### 11.1 PostgreSQL をジョブ状態の正本にする

既存の [抽出ジョブの設計判断](https://github.com/walnuts1018/horse/blob/main/docs/decisions/extraction-job-queue.md) では、PostgreSQL を正本、RabbitMQ を起床通知として使う判断が記録されています。

DMMF に照らしても、この考え方は合理的です。

Rust でも、DB のトランザクションで Job を claim し、外部 AI/OCR を呼び、結果を lease token などでフェンスして保存します。

RabbitMQ は残してもよいですが、互換性が不要なら、PostgreSQL polling と `LISTEN/NOTIFY` を組み合わせる案も十分検討できます。

何を使うかより、Job の状態と再実行保証を DB 上の業務モデルとして定義することが重要です。

## 12. GraphQL は Domain Model の公開手段ではなく、境界上の DTO として扱う

DMMF 第11章 §11.2–11.5 の DTO 境界を、そのまま GraphQL に適用します。

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

基本的な責務は以下です。

```

GraphQL Input
    ↓
Input DTO
    ↓
Parse / Validate
    ↓
Domain Command
    ↓
Application Workflow
    ↓
Workflow Result
    ↓
GraphQL Output DTO

```

GraphQL Resolver の中に業務計算や SQL を書かない設計です。

### 12.1 Mutation を Domain Command に対応させる

既存の `createAndPostPurchase`、`correctPurchase`、`closePeriod`、`recordTransfer` のような業務動詞に基づく Mutation は、DMMF と相性がよいです。

これをさらに一貫させます。

例えば、

```
type Mutation {
  postPurchase(input: PostPurchaseInput!): PostPurchaseResult!
}

union PostPurchaseResult =
    PostPurchaseSuccess
  | ValidationFailure
  | RevisionConflict
  | AccountingPeriodClosed
  | AlreadyPosted
```

のような設計です。

すべての Mutation に必ず個別のエラー union が必要ということではありません。ただし、`BAD_INPUT` や `CONFLICT` だけでは区別できない、ユーザーに意味のある失敗は公開契約で明示する価値があります。

予期しない DB 障害まで GraphQL のドメインエラー union に含める必要はありません。

### 12.2 GraphQL の Query は別モデルでよい

Mutation に厳密なドメイン型を使用する一方、Query は read projection から結果を組み立てられます。

例えば `homeSummary` は Purchase、Journal、FundingSchedule などをすべてドメイン集約としてロードするのではなく、集計 SQL や専用 projection で取得します。

GraphQL のネストしたフィールドの N+1 対策には、DataLoader も使えます。Rust の [async-graphql DataLoader](https://docs.rs/async-graphql/latest/async_graphql/dataloader/) はバッチ取得をサポートしています。

[image](https://www.google.com/s2/favicons?domain=https://docs.rs\&sz=32)

Rust

### 12.3 認可を Domain と Application の境界で扱う

OIDC の認証処理自体は Infrastructure 側ですが、Household に対する操作権限は業務上の制約です。

例えば `ClosePeriod` は、ただ有効な JWT を持っているだけでは実行できません。

Household の現在の所属と権限を確認したうえで、Command を実行します。

GraphQL の Node ID、HTTP レイヤーでの認証、DB の家庭分離を別々に守り、他家庭の識別子を知っていても操作できないことを保証します。

## 13. エラーハンドリング：Rust の Result に統一すればよいわけではない

DMMF 第10章の重要な主張は、エラーをすべて同じものとして扱わないことです。

Rust では `Result<T, E>` が標準ですが、エラーの型自体をドメインとして設計する必要があります。

例えば、

```
pub enum PostPurchaseError {
    InvalidPurchase(PurchaseValidationError),
    PaymentMismatch,
    RevisionConflict,
    PeriodClosed,
    AlreadyPosted,
}
```

のように、Workflow が予期する失敗だけを定義します。

それと別に Application 側には、

```
pub enum HandlerError {
    Domain(PostPurchaseError),
    Infrastructure(InfrastructureError),
    CorruptStoredState,
}
```

のような分類が考えられます。

重要なのは、DB 接続失敗と「締め済みの会計月へ記帳しようとした」を同じエラーにしないことです。

検証についても、ユーザー入力の複数の不正値はまとめて収集し、依存する業務処理の失敗は `?` で短絡評価する、といった使い分けができます。

これは第10章 §10.6–10.8 の Result 合成と Applicative Validation の区別に対応します。

## 14. 具体的な Rust 技術スタック

今回の目的なら、まず以下を候補にします。

| 関心事 | 推奨候補 | 理由 |
| --------------------- | --------------------------- | --------------------------- |
| HTTP | `axum` | 薄い HTTP 境界、Tower ミドルウェア |
| GraphQL | `async-graphql` | 型付き SDL・Resolver・DataLoader |
| Async Runtime | `tokio` | I/O と Worker 実行 |
| PostgreSQL | `diesel` 2.3 + `diesel-async` 0.9.2 | 型付き Query DSL と非同期 PostgreSQL I/O |
| Serialization | `serde` | DTO 専用に使用 |
| Domain Errors | 手書き enum、`thiserror` | 業務エラーの明示 |
| Infrastructure Errors | `thiserror` / `anyhow` | 境界で必要に応じて使用 |
| Date / Time | `time` | 時点と会計日・期間の区別 |
| IDs | `uuid` | 意味別 newtype でラップ |
| Property Testing | `proptest` | 不変条件の網羅的な検証 |
| Observability | `tracing` / `opentelemetry` | Workflow と I/O のトレース |
| Migrations | Atlas | 既存のスキーマ管理を継続 |

Dieselは`table!`で宣言したスキーマを使ってQuery DSLをコンパイル時に検査し、`diesel-async`は非同期のPostgreSQL接続とクエリ実行を提供します。`diesel print-schema`でAtlas適用後のスキーマから型定義を生成し、型付きQuery DSLでDBアクセスを実装します。アプリケーションコードでは生SQLを発行する`sql_query`を使いません。

[image](https://www.google.com/s2/favicons?domain=https://docs.rs\&sz=32)

Docs.rs

+2

DBアクセスにはDieselを採用します。DieselのQueryable型はInfrastructureに閉じ込め、Domain型と永続化DTOの変換をAdapterで行います。DBスキーマとmigrationはAtlasを正本とし、Dieselのmigration機能は使いません。DSLで表現できない処理に生SQLを追加せず、クエリやスキーマを見直します。

また、`fp-core` や `frunk` などの関数型ライブラリを、DMMF らしくするためだけに導入する必要はありません。Rust 標準の `Option`、`Result`、関数、enum、trait で大部分を実現できます。

Kubernetes への配備は、API、Worker、既存の OCR サービスを必要に応じて別コンテナにする構成でよいでしょう。Go の業務コードと API は完全に Rust へ置き換えつつ、推論ランタイムまで Rust 化する必要があるかは別の技術判断です。

## 15. テストも DMMF の方針で分割する

Rust 化による最大のメリットは、会計ロジックを DB なしで徹底的にテストできることです。

### Pure Domain Tests

例えば配賦について、任意の有効な入力に対して以下を保証します。

\\[ \sum\_{i=1}^{n} \operatorname{Allocate}(A,w)\_i=A \\]

さらに、配賦結果が非負であること、結果が決定的であること、オーバーフローしないことを確認します。

仕訳は借方と貸方が必ず一致し、貸付の返済は未返済元本を超えず、締め済み精算の結果が後続のルール変更によって変化しないことを検証します。

### Workflow Tests

依存する外部情報を値として与え、各 Command に対して、想定する状態遷移・失敗・イベントが返ることを検証します。

特に「不正状態を構築できない」ことは、実行時テストだけでなく Rust のコンパイル時テストでも確認できます。

### Infrastructure / Concurrency Tests

実際の PostgreSQL を用いて、同時更新、二重確定、月次締めと購入確定の競合、Worker lease 期限切れ、Outbox 再配送などを検証します。

ここは型だけでは保証できない部分です。

さらに、入力元別の契約テストを設けます。GraphQL、DB Row、外部 AI 応答、Worker メッセージの DTO から、有効な Domain 値へ安全に変換できることを確認します。

## 16. 最終的に採用したい設計判断の一覧

書籍の直接的な推奨と、Horse 固有の設計判断を分けて整理します。

| 論点             | 私の推奨                                    | 根拠・位置づけ                   |
| -------------- | --------------------------------------- | ------------------------- |
| 主分割単位          | Bounded Context                         | DMMF 第1・3章                |
| デプロイ           | Modular Monolith                        | DMMF 第3章に整合する Horse 固有の判断 |
| Domain         | 不変の struct / enum                       | DMMF 第4〜6章                |
| 制約の構築          | Smart Constructor                       | DMMF 第6章                  |
| Workflow       | Command → 型付き Pipeline → Result / Event | DMMF 第7〜10章               |
| I/O            | Pure Core の外側で実行                        | DMMF 第3・12章               |
| DI             | 小関数・狭い capability                       | DMMF 第8・9・12章             |
| Repository     | 汎用 Repository は採用しない                    | DMMF 第12章                 |
| エラー            | Workflow ごとの型付き Error                   | DMMF 第10章                 |
| DTO            | GraphQL・DB・Message と Domain を分離         | DMMF 第11章                 |
| 更新・読み取り        | 論理 CQRS                                 | DMMF 第12章                 |
| Event Sourcing | 原則不採用                                   | 書籍では必須でない。Horse 固有の判断     |
| RDB            | PostgreSQL                              | Horse の整合性要件からの判断         |
| 永続ジョブ          | DB 状態機械                                 | Horse 固有の判断               |
| イベント配送         | Transactional Outbox                    | DMMF の不足部分を補う設計           |
| 並行性            | DB ロック・revision・冪等性                     | Horse の非機能要件に基づく          |
| 型の進化           | モデル変更を先に行いコンパイルエラーを追う                   | DMMF 第13章                 |

## 17. 設計・実装に進む順序

移行コストを考慮する必要がないとしても、技術コンポーネントから実装し始めるのは勧めません。

DMMF の流れにできるだけ忠実に従うなら、以下の順序です。

1. Horse の業務イベントと Command を再定義する

   `PurchasePosted`、`PurchaseCorrected`、`AccountingPeriodClosed`、`FundingRecorded`、`LoanRepaid`、`ObservationConfirmed` などを整理し、各操作の前提・結果・禁止条件を書き出す。
2. Context Map を確定する

   Bookkeeping、Household、Catalog & Policy、Evidence & Ingestion の所有権と公開契約を決める。予算・定期支出は Policy 側のサブモデルとして検討し、独立した言語とライフサイクルが必要なら分離する。
3. 純粋なドメイン型・不変条件を設計する

   Money、Allocation、Journal、Purchase、Settlement、Period を、SQL や GraphQL を考慮せずに定義する。
4. 主要 Workflow の型シグネチャを先に書く

   `PostPurchase`、`CorrectPurchase`、`ClosePeriod` を優先し、入力、検証済み状態、必要な外部能力、出力、エラー、イベントを明文化する。
5. 関数合成と Workflow Tests を完成させる

   PostgreSQL を接続する前に、主要な状態遷移と会計計算が単独で検証できるようにする。
6. DB・GraphQL・Worker の契約を実装する

   Domain から要請される不変条件と状態遷移に合わせて DB を設計し、最後に入出力アダプターと実行環境を接続する。

これは一部機能だけを完成品としてリリースする順序ではなく、システム全体の要件を見据えながら、設計上の依存順序に従うという意味です。

## 18. 私が最も重視する3つの判断

長くなりましたが、Horse を DMMF にできる限り忠実に再実装するなら、特に以下の3点を守りたいです。

第一に、Workflow を Service メソッドの言い換えにしないこと。

`struct PurchaseService { db: PgPool }` を作り、そのメソッドへ既存 Go の処理を移すだけでは、Rust で書いた従来型のサービス層になります。

Workflow は、業務入力・途中状態・純粋な判断・出力・失敗を明確に持つ関数合成として表現します。

第二に、抽象化を増やすことを目的にしないこと。

DMMF は大量の Repository、Service、Strategy、Factory を作る方法論ではありません。

重要なのは、例えば「この Workflow は現在の Funding Rule を必要とする」「この段階で Closed Period を拒否する」という意味のある契約を、コードから読み取れることです。

第三に、Rust の型だけですべての正しさを保証しようとしないこと。

値単体の正しさは Smart Constructor、Aggregate の整合性は純粋な遷移関数、同時更新は DB Transaction、BC を跨ぐ整合性は公開契約と非同期プロセスで保証します。

この責務の分け方こそ、DMMF の設計思想に最も近いと思います。

私なら次の具体的な設計対象は Bookkeeping BC の `PostPurchase` Workflow にします。

Horse 固有の難しさである、複数品目・負担割合・複数決済・ルールのスナップショット・複式仕訳・冪等性・競合制御・証拠確定がすべて集中するためです。

ここで Domain Types、Workflow の関数シグネチャ、DI、Decision の返り値、Transaction Runner、GraphQL DTO の境界を筋の通った形にできれば、その設計を `CorrectPurchase`、`ClosePeriod`、`RecordTransfer` や他の BC へ適用する際の強い基準になります。

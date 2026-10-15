# Horse Rust 実装の DMMF 観点での設計レビュー

現時点の評価：方向性は良いが、構造的な再設計を推奨

結論から言うと、現状の Rust 実装には DMMF の重要な思想がすでに取り込まれています。ただし、「できるだけ厳密に DMMF に従う」という目標なら、今のうちに大きく変更したい点があります。

特に重要なのは、次の3点です。

1. `bookkeeping-domain` と `bookkeeping-workflows` の間でドメインモデルが二重化しており、真の業務モデルが後者に偏っていること。
2. Workflow が純粋な意思決定関数として実装されている一方、そのロード・実行・永続化を含む業務操作全体は PostgreSQL 実装の内部に埋め込まれていること。
3. Smart Constructor や型による不正状態の排除が一部に適用されているものの、業務上重要な構造にはまだ `String`、`bool`、`Option`、公開フィールドの組み合わせが多く残っていること。

この3点を正すと、DMMF の第5〜12章で説明されている設計にかなり近づけられます。

## レビュー対象

2026年10月16日時点で取得できた `main` の Rust ソースを確認しました。

参照 HEAD: [`a3603e2`](https://github.com/walnuts1018/horse/commit/a3603e2acf71d352b461d9ced275741312cfabc1)（2026年10月16日 01:48 JST 頃）

対象には `bookkeeping-domain`、各 `*-workflows`、`postgres`、`graphql-api`、Worker 関連、および既存の `docs/reviews/rust.md` を含めています。以下では、コードから確認できた事実と、書籍を踏まえた設計上の提案を区別します。

なお、今回は GitHub 上のソースレビューです。`cargo test` の実行や PostgreSQL を使った統合テストまでは実施していません。したがって、設計上の欠点と実際に再現するバグは区別して評価します。

## 1. 現状の良いところ

ここは残すべき設計思想です。

| 現在の実装 | DMMF の観点からの評価 |
| --------------------------------------- | ---------------------------------------- |
| `Yen` / `PositiveYen` | private field と Smart Constructor。第6章に合致 |
| `AllocationShares` | 非空・重複なし・正の重みを構築時に保証。良い |
| `BalancedJournal` | 借方と貸方の一致を型で表現。良い |
| `DraftPurchase` / `PostedPurchase` | ライフサイクルを異なる型で表現。第7章に合致 |
| `decide_post_purchase` | I/O を行わない純粋な意思決定関数。第12章に合致 |
| 楽観的な revision チェック | 型だけでは扱えない同時更新の整合性を補完 |
| DB Transaction 内でのイベント保存 | 業務更新とイベント記録を原子的に扱おうとしている |
| GraphQL の `Success` / `UserError` union | 予期される失敗を明示的に扱っている |

特に [money.rs](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/money.rs)、[allocation.rs](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/allocation.rs)、[journal.rs](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/journal.rs) は、今後のドメイン型実装の基本として活かせます。

また、[post_purchase_loader.rs](https://github.com/walnuts1018/horse/blob/main/crates/postgres/src/post_purchase_loader.rs) では購入と会計期間のロック取得があり、[post_purchase.rs](https://github.com/walnuts1018/horse/blob/main/crates/postgres/src/post_purchase.rs) では永続化時の再チェックが実装されています。

これは重要です。DMMF の「型で不正状態を排除する」を、データベースの競合制御まで型だけで実現できると誤解していません。

現状の問題は、関数型ドメインモデリングがないことではなく、適用が部分的で、アーキテクチャ全体の原則としてまだ徹底されていないことです。

## 2. 最優先で直したい設計上の問題

### P0-1. 本当の Purchase モデルが二重に存在している

該当コード：

- [bookkeeping-domain/src/purchase.rs](https://github.com/walnuts1018/horse/blob/a3603e2/crates/bookkeeping-domain/src/purchase.rs)
- [bookkeeping-workflows/src/lib.rs](https://github.com/walnuts1018/horse/blob/a3603e2/crates/bookkeeping-workflows/src/lib.rs#L28-L105)

現在、両方に `PurchaseId` があり、両方に `decide_post_purchase` があります。しかし、意味は同じではありません。

`bookkeeping-domain` 側は、`DraftPurchase` を `PostedPurchase` に遷移させ、`BalancedJournal` を保持するモデルです。

一方、`bookkeeping-workflows` 側は、Purchase の状態、品目、支払、負担配賦、資金負担スナップショット、仕訳明細までを扱う、実際の業務に近いモデルです。

この二重化は解消するべきです。

理想は、Bookkeeping BC に Purchase の意味を表すモデルを一つだけ持たせ、そのモデルに基づいて Workflow を定義することです。

`bookkeeping-domain` を実際の中核ドメインモデルに育て、`bookkeeping-workflows` にある `PurchaseId`、`PurchaseDraft`、`Payment`、`MemberFundingBasis`、仕訳関連の型を、意味に応じて移動・統合することを推奨します。

ここで重要なのは、単純に型を別ファイルへ移動することではありません。`BalancedJournal` と、実際の記帳 Workflow が返す `Vec<JournalLine>` を、同じ不変条件に基づくモデルとして再構成する必要があります。

### P0-3. 永続化状態を bool と汎用 enum の組み合わせで表現している

[bookkeeping-workflows/src/lib.rs L91–105, L240–249](https://github.com/walnuts1018/horse/blob/a3603e2/crates/bookkeeping-workflows/src/lib.rs#L91-L105) の構造では、

```
pub enum PurchaseStatus {
    Draft,
    Posted,
    Other,
}

pub struct PurchaseDraft {
    pub id: PurchaseId,
    pub revision: u64,
    pub status: PurchaseStatus,
}
```

という表現になっています。

`PurchaseDraft` なのに `status: Posted` にできます。さらに `Other` は、具体的な業務状態を隠してしまいます。

`accounting_period_open: bool`、`has_draft_purchases: bool` についても、業務上異なる意味の情報が単純なフラグとして渡されています。

改善するなら、次の3つを明確に区別したいです。

| 概念 | 望ましい型の例 |
| ------------------------ | ----------------------------------------------------- |
| 購入の永続的ライフサイクル | `Purchase::Draft` / `Purchase::Posted` |
| 会計期間のライフサイクル | `AccountingPeriod::Open` / `AccountingPeriod::Closed` |
| 記帳 Workflow で読み込んだ検証済み状態 | `PostingFacts` / `PostingReadyPurchase` |

特に、購入が Posted かどうかと、その購入を含む会計期間が Closed かどうかは別の状態軸です。

現状の [postgres/src/close_period.rs L480–492](https://github.com/walnuts1018/horse/blob/a3603e2/crates/postgres/src/close_period.rs#L480-L492) は、月次締め時に Purchase 自体を `CLOSED` に変更しています。

以前のレビューで指摘されていた締め後訂正の拒否は、現在の `correct_purchase.rs` では `Draft` だけを拒否するよう変更されており、その点は改善されています。

しかし、モデルとしては依然として Purchase と AccountingPeriod の状態が結合しています。DB まで作り直せるのであれば、この結合自体をなくす方が理想的です。

### P0-4. Workflow の責任と Transaction の責任が不明瞭

[postgres/src/category.rs](https://github.com/walnuts1018/horse/blob/a3603e2/crates/postgres/src/category.rs) では、PostgreSQL の `persist_create_category()` が `decide_create_category()` を呼び出しています。

[postgres/src/purchase_create_and_post.rs](https://github.com/walnuts1018/horse/blob/a3603e2/crates/postgres/src/purchase_create_and_post.rs) でも、作成・ロード・意思決定・記帳の結合を PostgreSQL Adapter が担当しています。

これらは直ちに不正な実装ではありません。特に `persist_create_and_post_purchase()` は同一トランザクション内で処理しており、以前のレビューで懸念されていた別接続間のロック問題を回避しています。

しかし、設計上は業務操作全体の定義が Infrastructure に存在しています。

DMMF により近づけるなら、PostgreSQL Adapter の公開責務は「DB 操作を提供すること」に寄せ、Application Workflow が業務操作の手順を明示し、その実行境界がトランザクションを所有する構造にしたいです。

ここには二つの正しさが必要です。

関数合成としての正しさ：どの事実を読み、どの型へ変換し、どの純粋関数で決定し、どの結果を発行するのか。

整合性としての正しさ：その過程で必要なロックとトランザクションを維持し、競合したら適切な業務失敗として扱うこと。

DMMF 第9章 §9.5–9.6 と第12章 §12.1–12.1.2 では、広い Repository よりも個別の I/O 関数を組み合わせる方式を示しています。

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

したがって、ここを修正するときに巨大な `PurchaseRepository` trait を追加するのは避けたいです。必要なロード・保存・外部照会能力を小さく定義し、それを Workflow の実行側で組み立てるのが適切です。

## 3. 現在のコードから確認できる、具体的な修正対象

構造の改善だけでなく、実際の整合性リスクや細部の型設計にも修正候補があります。

### P1-4. MemberId を PoolId として扱って配分している

該当：[bookkeeping-workflows/src/lib.rs L540–574](https://github.com/walnuts1018/horse/blob/a3603e2/crates/bookkeeping-workflows/src/lib.rs#L540-L574)

`MemberId` を文字列に戻し、`PoolId` として作り直して、同じ最大剰余法を利用しています。

動作は成立していても、概念上は異なる ID を偽装しています。

ここは配分アルゴリズムだけを型パラメータ化するのが適切です。

```
pub struct Weight<Id> {
    id: Id,
    value: NonZeroU64,
}

pub struct WeightedShares<Id> {
    weights: NonEmptyVec<Weight<Id>>,
}
```

例えば `WeightedShares<PoolId>` と `WeightedShares<MemberId>` に共通アルゴリズムを適用できます。

これは抽象化によって意味を失わずにコードを共有する、Rust らしい改善だと思います。

### P1-5. CreatePurchase にプリミティブ型が多い

該当：[bookkeeping-workflows/src/create_purchase.rs](https://github.com/walnuts1018/horse/blob/a3603e2/crates/bookkeeping-workflows/src/create_purchase.rs)

`CreatePurchaseCommand` には `occurred_at: String`、`date_precision: String`、`origin: String`、`allocation_rule_version: u64` などが存在します。

また、`Measure` は `numerator`、`denominator`、`unit: String` の組み合わせで、正当性を `decide_create_purchase` の内部で検証しています。

この設計では、検証済みの型としての保証が Workflow の後段まで残りません。

例えば、次の型を設けたいです。

```
pub enum DatePrecision {
    DateOnly,
    DateTime,
}

pub enum PurchaseOrigin {
    Manual,
    AiAssisted,
    Imported,
}

pub enum QuantityUnit {
    Piece,
    Gram,
    Kilogram,
    Milliliter,
    Liter,
}
```

数量の分子・分母は Smart Constructor を備えた正の有理数型にします。なお、`1 kg` と `1000 g` の扱いや単位変換は、業務要件に基づいて明示的に決定する必要があります。

また、`origin` のデフォルトが `source_document_id` の存在から `AI` と推定される点も見直したいです。原本が添付されていることと、AI が購入データを作成したことは同義ではありません。

この区別は、Evidence & Ingestion のモデリングでも重要になります。

### P1-6. ExtractionJob が不正な状態を作れる

該当：[bookkeeping-workflows/src/ingestion_job.rs](https://github.com/walnuts1018/horse/blob/a3603e2/crates/bookkeeping-workflows/src/ingestion_job.rs)

現状の `ExtractionJobState` は、`status` と `lease_token: Option<String>`、`lease_expires_at_ms: Option<i64>` を独立した公開フィールドとして持っています。

例えば `Running` なのに Lease が存在しない状態を構築できます。

永続的な Job 状態は、次のような判別共用体へ変更したいです。

```
pub enum ExtractionJob {
    Queued(QueuedJob),
    Running(RunningJob),
    Succeeded(SucceededJob),
    Failed(FailedJob),
    Cancelled(CancelledJob),
}

pub struct RunningJob {
    lease: ValidLease,
    attempt: AttemptNumber,
}
```

こうすると、`finish_extraction_success` の入力に `RunningJob` を要求できます。状態に合わない操作の多くを型で排除できます。

ただし、Lease がまだ有効であることや、別 Worker に奪われていないことは時間・DB 状態に依存するため、永続化時の条件付き更新も必要です。

この点は DMMF 第7章 §7.3 の永続状態遷移と §7.9 の長時間処理の考え方に直接対応します。

## 4. ディレクトリ・crate 構成も再設計したい

現在の Workspace は `bookkeeping-domain` と各 `*-workflows` が業務別になっている一方で、`postgres` と `graphql-api` が技術別の大きな共有モジュールになっています。

ここは、以前作成した設計方針にある「BC を第一の分割単位にする」を徹底したいです。

以下は書籍が指定する唯一の構成ではなく、Horse 向けの推奨構成です。

推奨する Rust Workspace

```
horse/
├── apps/
│   ├── api/                  # Composition Root
│   └── worker/               # Composition Root
│
├── contexts/
│   ├── bookkeeping/
│   │   ├── domain/
│   │   │   └── src/
│   │   │       ├── purchase/
│   │   │       ├── journal/
│   │   │       ├── allocation/
│   │   │       ├── funding/
│   │   │       ├── settlement/
│   │   │       └── period/
│   │   ├── application/
│   │   │   └── src/
│   │   │       ├── post_purchase/
│   │   │       ├── correct_purchase/
│   │   │       └── close_period/
│   │   ├── postgres/
│   │   │   └── src/
│   │   │       ├── write/
│   │   │       ├── read/
│   │   │       └── mapping/
│   │   └── contracts/
│   │
│   ├── household/
│   ├── catalog/
│   └── evidence/
│       ├── domain/
│       ├── application/
│       ├── postgres/
│       └── contracts/
│
├── adapters/
│   ├── graphql/
│   ├── identity/
│   ├── messaging/
│   ├── object-storage/
│   └── ai/
│
└── platform/
    └── postgres-common/      # Pool / TX 基盤のみ
```

重要なのは crate の数ではなく、依存方向です。

概念的な依存関係。Adapter は Domain・Application が定義する契約を実装し、Composition Root が組み立てます。

この構造なら、将来 PostgreSQL の実装を変更してもドメイン側の型・Workflow を変える必要がありません。また、Bookkeeping と Evidence が互いの内部構造を直接共有しないよう、crate 依存で制御できます。

`postgres-common` にドメイン固有の SQL を置かず、共通の接続管理・トランザクション基盤だけを置くことがポイントです。

なお、BC ごとに4つの crate を必ず作る必要もありません。意味のある公開境界を作れれば、単一 crate の非公開 module でもよく、過剰な crate 分割は避けるべきです。

## 5. 理想的な Workflow の組み立て方

ここが再実装全体の核になります。

### Purchase を例にした実行構造

GraphQL DTO → Command

デコード・形式検証・認証済み Actor

Application Workflow

トランザクション開始・権限確認・データロード

Pure Domain Decision

Validated Facts → PostedPurchase + Journal + Events

I/O なし

Persist Decision + Outbox

同一 Transaction 内で原子的に確定

Result DTO / Event Delivery

Commit 後にレスポンス・非同期配送

このような構造が望ましいと考えます。

Application Workflow はビジネス的な処理順序を明示し、純粋な Domain Decision は DB を認識しません。

Rust では、Dependency Injection を必ずしも大きな trait で実装する必要はありません。ローカルな能力を関数やクロージャで渡す設計を優先し、トランザクション境界や非同期ライフタイムの表現が必要な部分に限って小さな trait を利用するとよいです。

また、DB に依存する複数の処理を一つのトランザクションで行う必要があるため、Load → Decide → Persist の同じトランザクション内での実行を可能にする抽象化は必須です。

この抽象化を技術的な `UnitOfWork` として置くか、Workflow ごとの Transaction Runner として実装するかは Rust 側の選択ですが、DMMF の原則は I/O とドメイン判断の責任分離です。

### エラー型も Workflow ごとに設計する

現状の `InvalidDecision` は、アダプター内の異常をまとめる用途として多く登場します。

しかし、これが GraphQL に返す業務エラーへそのまま伝播すると、何が起きたのか分かりません。

例えば PostPurchase であれば、`PurchaseNotDraft`、`AccountingPeriodClosed`、`RevisionConflict`、`InvalidAllocation`、`PaymentMismatch` などを Workflow の公開エラーとして区別し、DB 接続障害やデータ破損とは別の階層に置きたいです。

DMMF 第10章が重視するのは、すべてのエラーを一つの `Result` 型へまとめることではなく、失敗の意味を保ったまま合成することです。

## 6. BC 境界とイベント契約

以前の設計方針では Household、Catalog & Policy、Bookkeeping、Evidence & Ingestion を候補としていました。

現在の実装を見ると、この分割はまだ十分にコード上の境界になっていません。

例えば `bookkeeping-workflows` の内部に `ingestion_job.rs` があり、`policy-workflows` には Budget、RecurringExpense、AllocationRule が混在しています。

ここは、単純に機能名を移動するのではなく、誰がその概念のルールを所有するのかを改めて決めたいです。

私なら、Bookkeeping BC が購入・仕訳・配賦確定・精算・会計期間の正本を所有し、Evidence BC が原本・抽出・照合候補・ジョブライフサイクルの正本を所有する方向にします。

商品・カテゴリの意味モデルは Catalog が所有し、Bookkeeping は購入時点の情報をスナップショットにします。

Budget と AllocationPolicy を独立 BC にするか、Bookkeeping 内のモジュールにするかは、独立した業務言語・変更頻度・データ所有があるかによって決めるべきです。現状の crate 名だけでは独立 BC の根拠にはなりません。

また、イベントも段階を分けます。内部ドメインイベント、BC 間の公開イベント、GraphQL Subscription 用の変更通知は、それぞれ異なる契約とするのがよいです。現在の `household_events` をそのまますべての用途に使うことは避けたいです。

DB にイベント行を書き込むことと、下流へ確実に配送したことも別です。必要な公開イベントは、Transactional Outbox を使って commit と配送の間を接続する設計を推奨します。ただし、配送は通常 at-least-once を前提に、購読側の冪等性も扱う必要があります。

## 7. GraphQL と永続化 DTO

GraphQL については、現在の `Success | UserError` union を維持して構いません。Domain Model と GraphQL Schema を分けている点は良いです。

一方、[graphql-api/src/category.rs](https://github.com/walnuts1018/horse/blob/a3603e2/crates/graphql-api/src/category.rs) では Resolver が `horse_postgres` を直接呼び出し、PostgreSQL 側の結果を GraphQL オブジェクトへ変換しています。

これは Query では必ずしも問題ではありません。DMMF 第12章の CQS/CQRS では、読み取りモデルをドメイン集約と同じ構造にする必要はないからです。

ただし Command 側は、GraphQL Resolver を薄い境界に留めて、Application Workflow を呼び出すようにしたいです。

さらに、DB から復元する際には、保存値を直接信用せず、必要な Smart Constructor と整合性検証を通します。

例えば現状の `catalog_workflows::category::Category` はすべて `pub` フィールドで `String` と `bool` を持っています。DB レコードから `From` で直接構築するため、カテゴリ名などの制約が永続化境界で再確認されません。

これは DMMF 第11章 §11.4、第12章 §12.5.3–12.5.4 に照らして改善余地があります。

## 8. さらに細かく改善したいところ

これらは大きな欠陥ではありませんが、理想的なコードベースを目指すなら手を入れたいです。

| 対象 | 改善 |
| ---------------------------------- | ---------------------------------- |
| `bookkeeping-workflows/src/lib.rs` | 794行の共通ファイルを役割別 module へ分解 |
| `PurchaseId` など | BC 内で正規の定義を一つに統一 |
| `PurchaseStatus::Other` | 実際の業務状態を明示する enum へ |
| `AccountingPeriod` | `"YYYY-MM"` 文字列ではなく `YearMonth` 型へ |
| `String::new()` による未指定表現 | `Option` または意味のあるデフォルト型へ |
| `Yen` / 金額の符号 | 非負金額・正金額・符号付き差額を用途別に統一 |
| `MemberFundingBasis` | 正規化されたルールを型として保持し、重みの再検証を減らす |
| `Category` の `archived: bool` | アーカイブ時に許可操作が変わるなら状態型を検討 |
| `event_type: &'static str` | ドメイン内では型付きイベントを使用 |
| `changed_fields: Vec<String>` | GraphQL 通知 DTO とドメインイベントを分離 |
| UUID の検証 | 同じ BC 内の ID 型で一貫した生成・復元規約を採用 |
| `Utc::now()` の直接使用 | 業務判断に日時が必要なら Clock を注入 |
| `expect()` の使用 | 数学的に保証された箇所だけ残し、その保証をテスト |
| `serde_json::Value` | 受信 DTO や永続化 DTO に閉じ込める |
| `InvalidDecision` | ドメインエラー・データ不整合・インフラ障害を区別 |

ただし、すべてを newtype にすればよいという話ではありません。書籍の第7章 §7.10 も型の過剰な増加には留保を付けています。

型を追加する判断基準は、業務上重要な誤りを新しく防げるかどうかに置くべきです。

## 9. 現時点で実施したい再設計の順序

変更コストを考慮しないなら、既存の構造のまま機能を増やすより、まず中核ドメインの正規化を行うことを推奨します。

01

Bookkeeping の Domain Model を一本化

Purchase・Journal・Payment・Allocation の型と不変条件を確定し、重複を解消

02

Workflow と Transaction Runner を分離

Load / Decide / Persist の責任・型・原子性を統一

03

状態遷移を型として再構成

Purchase・AccountingPeriod・ExtractionJob・Transfer の不正な組み合わせを排除

04

BC と Adapter の依存方向を確立

巨大な postgres crate を解体し、各 BC にデータ所有を対応させる

05

DTO・Event・Error 契約を整備

GraphQL・DB・非同期配送で別々の公開契約を定義

06

不変条件を自動テストで固定

純粋関数、状態遷移、競合、冪等性、永続化復元の各レベルを検証

テストについては特に、金額保存、貸借一致、精算の再現性を Property-Based Test で確認し、`Draft → Posted` などの遷移をテーブル形式で網羅したいです。

さらに PostgreSQL の実トランザクションを使って、同時記帳、月次締めとの競合、冪等再送、ロールバック、イベント保存の原子性を検証する必要があります。

## 10. 以前のレビューから何が変わったか

過去の [`docs/reviews/rust_review.md`](https://github.com/walnuts1018/horse/blob/a3603e2/docs/reviews/rust_review.md) と現在のコードの差も確認しました。

| 以前の指摘 | 現在の状態 |
| ----------------------------- | ----------------------------------- |
| 一部 Rust ファイルの module 登録漏れ | 確認対象の `lib.rs` では登録が追加済み |
| Create+Post の同一 Transaction 化 | `purchase_create_and_post.rs` で改善済み |
| 締め後訂正が `POSTED` 以外を拒否 | `Draft` のみ拒否する形へ変更済み。ただし状態モデルは未整理 |
| Domain と Workflow の型の配置 | 問題が残る |

したがって、古いレビューの修正項目を機械的に繰り返す必要はありません。現在は一段深い設計整理へ進める状況です。

## 最終的な推奨

今の Horse は、DMMF の考え方を取り入れた Rust アプリケーションとしては良い方向に進んでいます。

ただし、DMMF にできるだけ厳密に従うという目標であれば、現状の `*-workflows` にある業務型を本当のドメインモデルへ再編し、Workflow を単なる `decide_*` 関数の集合から、明示的に合成された業務操作へ引き上げることが最大の改善だと思います。

具体的には、`Purchase`、`Journal`、`AccountingPeriod`、`Transfer`、`ExtractionJob` のモデルをまず完成させ、それらが有効な状態・遷移しか持たないようにする。そのうえで DB と GraphQL を境界へ置く、という順序が最も理想に近いです。

Diesel、async-graphql、Tokio、Rust Workspace といった現在の技術選択そのものを変える必要性は、DMMF からは見いだせません。変更すべき中心はライブラリではなく、ドメインの型、責務の配置、Workflow の合成、そして境界の契約です。

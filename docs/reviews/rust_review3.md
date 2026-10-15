# Horse Rust 実装の DMMF 準拠レビュー

構造的な再設計を推奨

結論：現時点の実装は、DMMF の重要な考え方をかなり取り入れています。ただし、できる限り厳密に従うことを目標にするなら、今のアーキテクチャをそのまま拡張するのはおすすめしません。

問題は、関数型の考え方が欠けていることではありません。むしろ、純粋関数・Smart Constructor・状態型などは既に存在します。

最大の問題は、業務モデルが複数箇所に分散し、ドメインの型がそのまま業務操作全体の設計を支配する状態になっていないことです。

## レビューの前提

対象は [walnuts1018/horse](https://github.com/walnuts1018/horse) の `main` です。

確認 HEAD: [6afaff5](https://github.com/walnuts1018/horse/commit/6afaff568fe1a317d1e6a07d653f2206e28d6819) · 2026年10月16日時点

Rust workspace の構造、主要な Domain/Workflow、GraphQL、PostgreSQL、Worker、および既存の Rust 設計レビューと設計方針を確認しました。

今回の評価では、処理結果の業務的正しさ、計算式、SQL の性能、移行互換性は採点していません。また、ソース調査であり、`cargo test` や実DBを使った動作検証は行っていません。

以下では、書籍が明確に推奨していることと、私が Rust に適用する際の理想的な設計案を区別します。

## 1. 総合評価

| 評価軸 | 現状の判断 |
| ---------------------- | ------- |
| 純粋な Domain Decision | 良い |
| Smart Constructor | 部分的に良い |
| AND / OR 型モデリング | 部分的に良い |
| 不正状態の排除 | まだ不十分 |
| Workflow の境界 | 大きな改善余地 |
| Domain と永続化の分離 | 大きな改善余地 |
| Bounded Context の独立性 | 不十分 |
| 型による状態遷移 | 部分的に良い |
| Event / Command の意味モデル | 大きな改善余地 |
| GraphQL / DB の DTO 境界 | 部分的に良い |

### すでに良いところ

次の設計は維持したいです。

- [`Yen`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/money.rs)[/](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/money.rs)[`PositiveYen`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/money.rs) の非公開フィールドと Smart Constructor。
- [`AllocationShares`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/allocation.rs) の非空・重複なしという不変条件。
- [`BalancedJournal`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/journal.rs) の貸借一致という保証。
- `decide_*` によって業務判断を I/O から切り離そうとしている点。
- [`RecordTransferCommand`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/record_transfer.rs#L84-L89) が、操作種別ごとに異なるペイロードを持つ `enum` になっている点。
- [`PostingTarget`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/lib.rs#L296-L309) によって、購入記帳の対象が Pool または Account のどちらかであることを型で保証している点。

特に最後の二つは、既存のレビューから進んだ改善です。

## 2. 最優先で修正したい問題

### P0-1. Bookkeeping の真の Domain Model が二重化している

重要度：最優先

該当箇所：

- [`bookkeeping-domain/src/purchase.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-domain/src/purchase.rs)
- [`bookkeeping-workflows/src/lib.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/lib.rs)

現在、両方の crate に `PurchaseId` と `decide_post_purchase` が存在します。

`bookkeeping-domain` は次のようなモデルを持っています。

```
pub enum Purchase {
    Draft(DraftPurchase),
    Posted(PostedPurchase),
}
```

一方、実際の購入記帳に必要な明細、配賦、支払、資金負担などは `bookkeeping-workflows` にあります。

そして `bookkeeping-domain` の `decide_post_purchase` は `BalancedJournal` を使いますが、実際に PostgreSQL へ保存される仕訳は `bookkeeping-workflows` の `JournalLine` に基づいています。

つまり、型として立派に設計した Domain Model と、実際の業務を動かすモデルが一致していません。

これは DMMF 第5章 §5.3–5.8、第7章 §7.2–7.4 の観点から、非常に大きな改善対象です。

私なら次の状態を目指します。

- Purchase の業務上の同一性を表す型は Bookkeeping 内で一つにする。
- ライフサイクル状態をドメイン型として統一する。
- 記帳 Workflow が同じ `BalancedJournal` の保証を使用する。
- 各 Workflow は共通の正しい Domain Model から始まり、意味のある新しい状態を返す。

単に `PurchaseId` を共通ファイルに移すだけでは不十分です。

実際に使われるモデルを唯一の Domain Model にすることが必要です。

### P0-2. Workflow の Decision がドメインの結果ではなく、DB 更新用データになっている

重要度：最優先

非常にわかりやすい例が [`policy-workflows/src/budget.rs`](https://github.com/walnuts1018/horse/blob/main/crates/policy-workflows/src/budget.rs#L53-L65) です。

```
pub struct BudgetDecision {
    pub actor_id: String,
    pub id: String,
    pub household_id: Option<String>,
    pub expected_revision: Option<i64>,
    pub category_id: Option<String>,
    pub name: Option<String>,
    pub amount_yen: Option<i64>,
    pub starts_on: Option<NaiveDate>,
    pub ends_on: Option<Option<NaiveDate>>,
    pub archived: Option<bool>,
}
```

この設計には二つの問題があります。

第一に、Create / Update / Archive という異なる業務操作の結果を、大量の `Option` を持つ一つの構造体に押し込んでいます。

第二に、この Decision は有効な業務結果というより、SQL に渡す変更項目の集合になっています。

実際に [`postgres/src/policy.rs`](https://github.com/walnuts1018/horse/blob/main/crates/postgres/src/policy.rs#L29-L71) では、作成時に必ず必要なはずの `name`、`amount_yen`、`starts_on` を `Option` から取り出し、存在しなければ `InvalidWorkflow` にしています。

これは Decision が型によって正しさを保証できていない証拠です。

DMMF 的には、少なくとも以下のような方向が自然です。

```
pub enum BudgetChange {
    Created(Budget),
    Updated(Budget),
    Archived(ArchivedBudget),
}
```

ただし、これは概念例です。実際には必要な更新前 revision やイベントを含めて設計します。

重要なのは、存在して当然のフィールドを `Option` にして、後で存在確認する必要がないモデルにすることです。

同様の問題は [`update_purchase_item.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/update_purchase_item.rs#L9-L29) にもあります。

`quantity_set: bool` と `quantity: Option<Measure>` の組み合わせや、`changed_fields: Vec<String>` による変更指定は、GraphQL の入力 DTO としては理解できます。しかし、業務操作の中心にもそのまま残すより、型付きの変更コマンドへ変換したいです。

たとえば、値の保持・削除・変更を区別する必要があるなら、専用の `enum` を使えます。

書籍根拠： 第4章 §4.3、第6章 §6.3–6.4、第7章 §7.2、第11章 §11.2。

### P0-3. 業務判断が PostgreSQL Adapter に残っている

重要度：最優先

該当箇所：

- [`purchase_command_context.rs`](https://github.com/walnuts1018/horse/blob/main/crates/postgres/src/purchase_command_context.rs#L100-L185)
- [`create_purchase.rs`](https://github.com/walnuts1018/horse/blob/main/crates/postgres/src/create_purchase.rs#L389-L645)

現在の配賦ルールの選択は、おおむね Product Variant → Product Concept → Category → Merchant という優先順位で DB を検索して決めています。

ここで問題なのは優先順位の正しさではありません。

その優先順位自体が業務ポリシーなのに、SQL アクセス処理に埋め込まれていることです。

理想的には、PostgreSQL は候補となるルールを読み込み、ドメイン関数が優先順位を評価します。

```
pub fn select_allocation_rule(
    context: AllocationContext,
    candidates: AllocationRuleCandidates,
) -> Result<SelectedAllocationRule, AllocationError>
```

さらに重要なのが、[`validate_catalog_references`](https://github.com/walnuts1018/horse/blob/main/crates/postgres/src/create_purchase.rs#L389-L576) です。

この関数は `&mut CreatePurchaseDecision` を受け取り、Catalog の問い合わせ結果に応じて Decision 内の `category_id` を変更しています。

これは単なる永続化形式への変換ではありません。業務上の意味を持つ値の補完・解決です。

私なら、必要な参照データをロードした後に、純粋関数で最終的な Decision を生成する流れに変えます。

```
Command
  ↓
外部データのロード
  ↓
参照データの検証・解決
  ↓
Pure Decision
  ↓
永続化
```

ただし、これは「一つの Workflow の中では絶対に I/O と純粋関数を交互に置いてはいけない」という意味ではありません。DMMF 第12章 §12.1.1 は、必要であれば I/O と純粋ステップを交互に実行する構成も認めています。

大切なのは、業務判断を Infrastructure に隠さないことです。

### P0-4. Application Workflow の置き場所が統一されていない

重要度：高

現在、業務操作全体の実行責務が複数の場所に分散しています。

たとえば：

- [`graphql-api/src/purchase_mutations.rs`](https://github.com/walnuts1018/horse/blob/main/crates/graphql-api/src/purchase_mutations.rs#L619-L720) は transaction を開始し、load → decide → persist を実行します。
- [`postgres/src/close_period.rs`](https://github.com/walnuts1018/horse/blob/main/crates/postgres/src/close_period.rs#L85-L243) は DB アダプター内で業務操作全体を実行します。
- [`postgres/src/policy.rs`](https://github.com/walnuts1018/horse/blob/main/crates/postgres/src/policy.rs#L29-L141) は Decision の呼び出しと永続化を一緒に行います。

結果として、業務操作の全体像が GraphQL と PostgreSQL に分散しています。

DMMF 第3章 §3.4、第9章 §9.5–9.6、第12章 §12.1 に照らすと、改善すべき点です。

私なら、技術別の crate よりも先に、ユースケースの実行を一つの場所で読むことができる構造を作ります。

```
GraphQL Adapter
      ↓
Application Workflow
      ├── Load（I/O）
      ├── Validate / Decide（Pure）
      ├── Persist（I/O）
      └── Publish / Schedule（I/O）
      ↓
GraphQL DTO
```

これは Domain と Application の層を厳密に分けたいという Rust 側の設計提案であり、書籍が特定の `application` crate を要求しているわけではありません。

### P0-5. 重要な不変条件が他の Workflow ではまだ型になっていない

重要度：高

購入記帳の `PostingTarget` は改善されていますが、同じ原則が他の Workflow にまだ波及していません。

[`record_transfer.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/record_transfer.rs#L123-L132) では次のようになっています。

```
pub struct JournalLine {
    pub line_no: u32,
    pub side: PostingSide,
    pub account_id: Option<AccountId>,
    pub pool_id: Option<PoolId>,
    pub member_id: Option<MemberId>,
    pub amount: PositiveYen,
    pub memo: &'static str,
}
```

これは、三つの独立した `Option` により、8種類の組み合わせを表現できます。

コードには対象IDを持たない行も実際に登場するため、それ自体を不正だと断定することはできません。

しかし、それが正当な会計上の概念なら、対象IDなしというデータ表現ではなく、その意味を持つバリアントにするほうが DMMF 的です。

同様に [`correct_purchase.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/correct_purchase.rs#L88-L96) にも `pool_id` と `account_id` の組み合わせが残っています。

また、Journal の各 Workflow がそれぞれ異なる明細型を持っている一方で、既存の `BalancedJournal` に統一されていません。

ここは一度、Bookkeeping BC 全体の Journal モデルを作り直したいです。

## 3. 次に改善したい問題

### P1-1. `PurchaseDraft` がまだ無効な状態を表現できる

[`bookkeeping-workflows/src/lib.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/lib.rs#L93-L106) では：

```
pub struct PurchaseDraft {
    pub id: PurchaseId,
    pub revision: u64,
    pub status: PurchaseStatus,
}
```

Draft と名前が付いているのに、`status` に `Posted` や `Closed` を渡せます。

そのため、`decide_post_purchase` で改めて Draft かどうかを確認しています。

ここは典型的な、型で排除できる不正状態です。

```
pub struct DraftPurchase {
    id: PurchaseId,
    revision: Revision,
    // ...
}
```

のように、Draft であることを型で表すほうが自然です。

ただし、会計期間の Closed と購入自身の Posted が独立した状態軸であるなら、別の型に分けるべきです。すべてを一つの Purchase enum に押し込む必要はありません。

書籍根拠： 第6章 §6.3、第7章 §7.2–7.3。

### P1-2. MemberId を PoolId として使っている

[`bookkeeping-workflows/src/lib.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/lib.rs#L626-L658) では、Member ID をいったん `PoolId` として生成し、`AllocationShares` のアルゴリズムに渡しています。

計算上の都合は理解できます。しかし、DMMF の Value Object の目的は、意味の違う値を型で区別することです。

ここでは汎用の重み付き配分アルゴリズムを作るなら、キー型を抽象化するほうがよいと思います。

```
pub struct WeightedShares<K> {
    shares: Vec<WeightedShare<K>>,
}
```

`K` に `MemberId` または `PoolId` を使えば、概念の偽装が必要なくなります。

これは汎用化そのものが目的ではありません。意味の異なる識別子を相互変換しないための抽象化です。

### P1-3. Workflow の入力に DTO 的なプリミティブが残りすぎている

[`create_purchase.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/create_purchase.rs#L6-L60) の入力は、`String`、`i64`、`Option<String>` などが中心です。

`Measure` も分子・分母・単位を公開しており、検証は Workflow 内の関数で行われます。

未検証入力がプリミティブなのは問題ありません。

問題は、それが検証後もそのまま次のステップへ流れ、各関数が同じ意味を繰り返しチェックする状態です。

理想は：

```
UnvalidatedCreatePurchase
        ↓ validate
ValidatedCreatePurchase
        ↓ allocate
AllocatedPurchase
        ↓ decide
CreatePurchaseDecision
```

のように、意味ある処理段階を型として表現することです。

すべての中間値を型にする必要はありませんが、「検証済みかどうかで許される操作が変わる」なら、別型にしたいです。

### P1-4. Event が業務上の事実ではなく、変更通知の形式に寄っている

[`DomainEvent`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/lib.rs#L394-L400) は次のような構造です。

```
pub struct DomainEvent {
    pub event_type: &'static str,
    pub entity_id: PurchaseId,
    pub revision: u64,
    pub changed_fields: Vec<&'static str>,
}
```

この形式は GraphQL のクライアント更新通知には便利です。

しかし、`purchase.posted` と `changed_fields = ["status"]` は、購入の記帳という業務事実の意味を十分に表した契約とは限りません。

私なら少なくとも以下を区別します。

- `PurchasePosted` のような業務上の事実
- 他の Bounded Context に公開するイベント DTO
- GraphQL クライアント用の変更通知

DMMF 第3章 §3.4、第13章 §13.3.6 が重視するのは、公開する契約の意味と消費者の関心事です。

既存の `household_events` を捨てる必要はありませんが、それを Domain Event そのものと同一視しないほうがよいです。

### P1-5. ExtractionJob の状態型がまだ完全ではない

[`ingestion_job.rs`](https://github.com/walnuts1018/horse/blob/main/crates/bookkeeping-workflows/src/ingestion_job.rs) では、`QueuedJob`、`RunningJob`、`FinishedJob` を区別するようになっています。

これは良い改善です。

一方、`ValidLease` のフィールドは公開されており、任意の文字列や期限値から直接構築できます。また `FinishedJob` の `finished_at_ms` は `Option<i64>` です。

そのため、型の名前が約束している保証を、公開コンストラクション経路が必ずしも守っていません。

さらに、`FinishExtractionJob` は `status` と `finished_at_ms` を独立させています。

長時間の Workflow に対しては、DMMF 第7章 §7.9 の Saga / Process Manager の考え方を使い、状態遷移と永続化される進捗を明示的に設計したいです。

`ExtractionJob` は Bookkeeping よりも、Evidence / Ingestion のドメインに属する可能性も高いと思います。

### P1-6. Workflow Error が境界で過度に集約されている

[`purchase_mutations.rs`](https://github.com/walnuts1018/horse/blob/main/crates/graphql-api/src/purchase_mutations.rs#L1336-L1341) では、PostPurchase の Workflow 失敗をペイロードのない `PostMutationError::Workflow` に変換しています。

その後、すべて Conflict 相当として返しています。

GraphQL 上で複数のエラーを一つにまとめること自体は問題ありません。

しかし、それは公開 API の明示的な方針として行うべきであり、Application の内部ですべての失敗理由を失う必要はありません。

Workflow ごとの業務エラーを保ったまま、GraphQL アダプターで公開エラーへ変換するのが望ましいです。

書籍根拠： 第10章 §10.1–10.5。

## 4. Bounded Context と依存方向も見直したい

現状の workspace は `bookkeeping-domain`、`bookkeeping-workflows`、`catalog-workflows`、`policy-workflows`、`household-workflows` と、技術別の `postgres`、`graphql-api` などに分割されています。

構造自体が悪いわけではありません。

ただし、Bookkeeping 以外の BC では Domain と Workflow の意味上の区別があまり明確でなく、`postgres` には全 BC の I/O 実装が混在しています。

さらに、Bookkeeping の購入処理が Catalog テーブルの内部構造を直接参照しています。

もし Catalog を独立した BC と位置づけるなら、これは境界の所有権を弱めます。

私が理想とするのは、次のような構造です。

推奨する論理構造

Delivery / Composition Root

GraphQL · HTTP · Worker · 依存関係の組み立て

Bookkeeping BC

Application

Load → Decide → Persist

Domain

Model · Pure Functions · Events

Catalog BC

Application

Load → Decide → Persist

Domain

Model · Pure Functions · Events

Policy BC

Application

Load → Decide → Persist

Domain

Model · Pure Functions · Events

Household BC

Application

Load → Decide → Persist

Domain

Model · Pure Functions · Events

Infrastructure Adapters

PostgreSQL · 外部 API · メッセージ配送

矢印は実行時の主な呼び出し方向を表す概念図です。コンパイル時の依存方向は Domain が Infrastructure を参照しないようにします。

これは新たなマイクロサービス化を要求する設計ではありません。単一 Rust プロセスの Modular Monolith でも十分です。

また、すべての BC を必ず Domain / Application / Infrastructure の3 crateに分割する必要はありません。重要なのは、モジュール境界と依存方向が守られることです。

書籍根拠： 第1章 §1.4–1.5、第3章 §3.1–3.5、第12章 §12.3。

## 5. 私なら採用する具体的な設計方針

### Domain と Workflow

Bookkeeping の真のドメインモデルを再構築し、同じ型をすべての Bookkeeping Workflow が使うようにします。

`DraftPurchase`、`PostedPurchase`、`AccountingPeriod`、`BalancedJournal`、`FundingRule`、`Allocation` の責務と不変条件を整理します。

Workflow は単に `Command -> Decision` を一つ作ればよいのではなく、必要な検証・状態遷移・結果生成が関数契約として読めるようにします。

### Application と I/O

Application は業務操作の調整役です。DB から必要な値を読み出し、Smart Constructor で内部モデルを復元し、Domain の純粋関数を呼び出して、その結果を永続化します。

DB 更新を担うのは Infrastructure ですが、どの業務操作をどう進めるかという判断は Application / Domain に置きます。

DMMF 第12章 §12.1.2 に従えば、巨大な Repository trait を作るより、小さな `load_*` / `save_*` 関数を組み合わせる構成が適しています。

### DTO の境界

GraphQL の InputObject、DB レコード、内部 Domain Model を明確に分離します。

`String` や `Option` を含む外部データのモデルは許容し、それを内部へ取り込む際に、必要な制約型へ変換します。

永続化後に読み出した値も無条件には信用せず、同様に再構築します。

書籍根拠： 第11章 §11.2–11.5、第12章 §12.5。

## 6. 大きく作り直す場合のディレクトリ案

個人的には、現在の `*-workflows` と横断的な巨大な `postgres` crate を中心にする構成より、次のような境界を優先したいです。

```
crates/
  contexts/
    bookkeeping/
      domain/
        src/
          model/
          workflows/
          events.rs
          errors.rs
      application/
        src/
          commands/
          queries/
      postgres/
        src/
          commands/
          queries/
          mapping/

    catalog/
      domain/
      application/
      postgres/

    policy/
      domain/
      application/
      postgres/

    household/
      domain/
      application/
      postgres/

    ingestion/
      domain/
      application/
      postgres/

  delivery/
    graphql/
    http/
    worker/

  integrations/
    gemini/
    object-store/
    email/

apps/
  api/
  worker/
```

ここでの `domain` は、共通の型と純粋な Workflow を保持します。`application` は外部 I/O を含む業務操作の実行を調整します。

Domain に依存する Application と、Domain の内部に保持する Workflow を区別することで、DMMF の関数合成と I/O 境界を両立できます。

また、`bookkeeping`、`catalog`、`policy`、`household`、`ingestion` という BC 分割は現段階の仮説です。確定には、それぞれの用語・責務・整合性・データ所有の確認が必要です。

特に Policy と Bookkeeping の関係は、業務モデルの独立性によっては統合または別の分割もあり得ます。

## 7. 修正の優先順位

大規模変更が許されるなら、次の順序にします。

| 順番 | 実施内容 | 目的 |
| -- | ---------------------------- | ------------- |
| 1 | BC の境界と用語の整理 | モデルの所有者を確定 |
| 2 | Bookkeeping Domain Model の統一 | 二重モデルを解消 |
| 3 | Workflow の入出力型の再設計 | SQL用データ構造から脱却 |
| 4 | Smart Constructor / 状態型の徹底 | 不正状態を排除 |
| 5 | Application Workflow の分離 | I/O と純粋判断を分離 |
| 6 | Journal / Event のモデル統一 | 業務上の意味を明確化 |
| 7 | PostgreSQL の BC 別分割 | データ所有境界を明確化 |
| 8 | GraphQL / Worker 境界の整理 | 外部契約と内部モデルを分離 |

ここで重要なのは、ファイルを先に移動しないことです。

最初に型と業務操作の意味を確定し、その結果としてディレクトリ構造を決めるのが DMMF に最も忠実だと思います。

## 8. 修正しなくてもよいもの

厳密に DMMF を守ろうとして、逆に過剰設計になりやすい部分もあります。

Repository trait を全エンティティに作る必要はありません。すべての Workflow を一つの巨大な純粋関数にする必要もありません。すべての小さな値に newtype を導入する必要もありません。CQRS のために DB を二つ持つ必要もありません。

DMMF 第7章 §7.10 は型の過剰さに留保を置いていますし、第12章 §12.1.2 は Repository パターンを必須としていません。

「型を増やすこと」ではなく、実際の業務上の制約をコンパイラーで表現できる範囲を最大化することを目標にするのがよいです。

## 最終評価

Horse の Rust 実装は、DMMF の個々のテクニックを採用する段階から、DMMF の設計思想によってシステム全体の構造を決める段階に移るべきだと考えます。

そのために、現時点で最も価値があるのは以下の三つです。

第一に、Domain Model を一元化すること。 `bookkeeping-domain` が単なる一部の純粋関数・値オブジェクト集ではなく、Bookkeeping 全体の意味を定義する場所になるべきです。

第二に、Decision の型を業務上の結果として設計すること。 `BudgetDecision` のような、大量の Option を持つ DB 更新用の構造体から、意味を持つ状態・遷移・イベントのモデルへ改めるべきです。

第三に、Application の実行境界を確立すること。 PostgreSQL や GraphQL の内部に業務操作全体が隠れず、Command から結果に至る流れを独立して理解できるようにすべきです。

この三つを修正すれば、以降の実装では個別の型設計・関数合成・エラー設計・永続化変換が、自然に DMMF の方向へ揃いやすくなります。

現時点であれば、ディレクトリ構造も crate の境界も含めて全面的に再設計する価値が十分にあります。 特に Purchase / Journal 周辺を最初の完成度の高い手本として作り、ほかの Workflow に展開する方針を推奨します。

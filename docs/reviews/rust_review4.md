# Horse Rust実装のDMMF準拠レビュー

対象: [walnuts1018/horse](https://github.com/walnuts1018/horse) 基準: Domain Modeling Made Functional（DMMF）全13章の本文、および前回作成した書籍分析 確認対象: `main`、コミット [`b3218b75`](https://github.com/walnuts1018/horse/commit/b3218b75e8a0dbad28c134ccd43fda91e749ae20)（2026年10月16日 03:25 JST時点）

## 総評

現状のRust実装はDMMFの方向に進んでいます。ただし、DMMFをできるだけ厳密に実践するという目標なら、今の段階で設計を見直す価値が十分にあります。

特に重要なのは、ドメインロジックを純粋関数にするところまでは進んでいる一方で、その関数が受け取る値、返す値、Bounded Contextの境界には、まだDMMFの型駆動設計を徹底できる余地があることです。

良い点は明確です。

- `Yen`、`PositiveYen`、`BalancedJournal` などにSmart Constructorがある。
- `Purchase::Draft` / `Purchase::Posted` でライフサイクルを区別している。
- `decide_post_purchase` のように、外部I/Oを伴わない判断関数を定義している。
- `PostPurchaseDecision` がイベントを値として保持している。
- GraphQL、Application、Domain、PostgresをCargo workspace内で分離し始めている。

これらは、書籍の第5〜7章、第9〜12章に沿った良い選択です。

しかし、特に次の2点は優先的に改善したいです。

## 1. 検証済みの値が、型として保証されていない

重要度：高 ／ DMMF第5章 §5.3、第6章 §6.1〜6.4、第7章 §7.2、第11章 §11.4

### 実際のコード

[bookkeeping/application/src/create_purchase.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/application/src/create_purchase.rs#L115-L142) には、次のような入力が存在します。

```
pub struct CreatePurchaseItem {
    pub item_id: String,
    pub name: String,
    pub amount_yen: i64,
    // ...
}
```

`decide_create_purchase` は、ID、名称、金額などを検証します。

ところが、検証後の `DecidedPurchaseItem` でも、これらは基本的に `String` や `i64` のままです。さらにDecisionのフィールドも公開されているため、検証関数を通さずに、別のコードから不正なDecisionを構築できます。

これはDMMFの重要な原則である Make Illegal States Unrepresentable（不正な状態を表現できなくする） を十分に活かせていません。

### 理想的な方向

検証前後の型を明確に分けます。

```
pub struct UnvalidatedPurchaseItem {
    pub name: String,
    pub amount_yen: i64,
}

pub struct ValidatedPurchaseItem {
    name: PurchaseItemName,
    amount: Yen,
}

pub struct PurchaseItemName(String);

impl PurchaseItemName {
    pub fn parse(value: String) -> Result<Self, NameError> {
        // 検証後のみ構築
        todo!()
    }
}
```

`ValidatedPurchaseItem` は公開するフィールドを持たず、検証関数やSmart Constructorだけが生成できるようにします。

ただし、Command全体を必ず非公開フィールドにする必要はありません。未検証のCommandは不正値を表現できてもよい一方、検証済みとして扱う型には保証が必要、という区別が本質です。

また、金額単体の正当性と、明細合計・支払い合計などの集合全体の整合性は別です。後者も必要なら集約単位の構築経路で保証するべきです。

## 2. アプリケーション層が、永続化表現を知っている

重要度：高 ／ DMMF第3章 §3.5、第11章 §11.2〜11.4、第12章 §12.1〜12.5

[同じファイルの冒頭](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/application/src/create_purchase.rs#L20-L105) には、以下のような実装があります。

```
impl PurchaseOrigin {
    pub const fn as_db_value(self) -> &'static str {
        match self {
            Self::AiAssisted => "AI",
            Self::Manual => "MANUAL",
            Self::Unknown => "UNKNOWN",
        }
    }
}
```

`DatePrecision` や `QuantityUnit` にも類似のDB変換があります。

Domain/Application層の型がPostgreSQLの保存形式を知る必要はありません。

DMMFに沿うなら、`PurchaseOrigin` は業務上の意味を表すだけにし、`AI`、`MANUAL` といった保存上の値への変換はPostgres Adapterが所有するのが自然です。

これは小さな修正ですが、重要です。将来DB側の表現が変わったときに、ドメインの意味モデルまで変更する設計を避けられます。

この2点は、現在の実装から具体的に確認できる改善事項です。

## 3. `Validated*` という名前でも、不正状態を構築できる

重要度：高 ／ DMMF第4章 §4.3、第6章 §6.3〜6.4、第7章 §7.2

特に分かりやすい例が、[policy/application/src/set_allocation_rule.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/policy/application/src/set_allocation_rule.rs#L3-L42) です。

```
pub struct AllocationRuleTarget {
    pub category_id: Option<String>,
    pub merchant: Option<String>,
    pub product_concept_id: Option<String>,
    pub product_variant_id: Option<String>,
}
```

この型は、4つのフィールドがすべて `None` の場合も、複数が `Some` の場合も表現できます。

実際、`validate_set_allocation_rule` では、選択されたターゲットの数が1つであることを実行時に検証しています。

しかし、このモデルはまさにDMMFがOR型で表現するよう勧めているものです。

```
pub enum AllocationRuleTarget {
    Category(CategoryId),
    Merchant(MerchantName),
    ProductConcept(ProductConceptId),
    ProductVariant(ProductVariantId),
}
```

これなら「対象がちょうど1つ」という制約を型自体が保証します。

さらに、現在の `ValidatedSetAllocationRuleCommand` はフィールドがすべて `pub` のため、検証を迂回できます。`AllocationRuleWeight` も同様に、検証後の型でも不正な重みを直接構築できてしまいます。

名前としてのValidatedと、型システムが保証するValidatedを一致させるべきです。

これは書籍への厳密な準拠という意味でも、優先度の高い変更です。

## 4. Bounded Contextの境界とデータ所有権が、まだ一致していない

重要度：最優先 ／ DMMF第1章 §1.4〜1.5、第3章 §3.1〜3.4、第12章 §12.3、第13章 §13.3.6

現在のディレクトリ構造では、`bookkeeping`、`catalog`、`household`、`policy`、`evidence` がコンテキストとして表現されています。

問題は、コード上の依存とデータアクセスが、その境界をまだ十分に守っていないことです。

たとえば、[bookkeeping/postgres/src/purchase_command_context.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/postgres/src/purchase_command_context.rs#L74-L124) は、Bookkeepingの処理でありながら、`households`、`product_variants`、`product_concepts`、`allocation_rules` などを直接参照しています。

また、[cross-context/postgres/src/pool_mutations.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/cross-context/postgres/src/pool_mutations.rs#L30-L153) は、プールの作成と同じトランザクションで、家計のリビジョンやデフォルト負担設定も更新しています。

問題は「同じDBにアクセスしていること」そのものではありません。DMMFでは物理DBの共有は許容されています。

問題は、別々のBounded Contextだと考えている領域が、実際には互いの永続化モデルと更新規則を直接知っていることです。

この状態では、ディレクトリを分けただけではBCの独立性を実現できません。

改善には2つの可能性があります。

第一に、現在別BCとみなしている機能が、本当は同一の整合性境界に属するなら、BCを再統合します。

第二に、独立したBCとして成立するなら、公開された問い合わせ・Command・Eventによって連携し、必要な意味の変換をAnti-Corruption Layerに置きます。

どちらが正しいかは業務モデルによります。DMMFに厳密であることは、何でも小さいBCに分割することではありません。

これはディレクトリ構造の整理よりも先に決めるべき、根本的な設計問題です。

## 5. PostgreSQL Adapterに業務上の判断が残っている

重要度：最優先 ／ DMMF第3章 §3.5、第9章 §9.5〜9.6、第12章 §12.1

これは単なるレイヤー依存以上に、本質的な問題です。

[bookkeeping/postgres/src/purchase_command_context.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/postgres/src/purchase_command_context.rs#L149-L205) では、負担ルールの候補をProduct Variant、Product Concept、Category、Merchantの順に検索し、最初に見つかったものを採用しています。

この「どのルールを優先するか」は、SQLの技術的な関心ではなく業務上のポリシーです。

より適切な分担は次の形です。

PostgreSQL Adapter

必要なルール候補を読み取る

Pure Domain Function

候補から業務上適用すべきルールを決定する

RuleCandidates → SelectedRule

Purchase Workflow

選ばれたルールを用いて配賦し、結果を構築する

さらに、[bookkeeping/postgres/src/create_purchase.rs の](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/postgres/src/create_purchase.rs#L385-L476)[`validate_catalog_references`](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/postgres/src/create_purchase.rs#L385-L476) は、`&mut CreatePurchaseDecision` を受け取り、Catalogを参照した結果で `category_id` を変更しています。

Pure Decisionが生成した結果を、永続化側が後から業務的に変更しているわけです。

これではDecisionが最終的な業務判断を表せていません。

参照値の取得はAdapterが担当して構いませんが、その結果を用いた選択・補完・最終Decision生成は純粋な関数に戻すべきです。

一方、同時更新防止のためのrevision照合、行ロック、冪等性、DBの制約確認まで純粋関数へ移す必要はありません。これらはI/O境界で扱うべき整合性の責務です。

## 6. Workflowの全体像がGraphQL・Application・Postgresに分散している

重要度：高 ／ DMMF第3章 §3.4、第7章 §7.4〜7.8、第9章 §9.6、第12章 §12.1

現在の実装には、Workflowの構成場所が異なるパターンが混在しています。

[create_and_post_purchase.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/application/src/create_and_post_purchase.rs#L164-L251) では、Applicationが外部能力を受け取り、Load・Decision・Persistを構成しています。これは良い方向です。

しかし [purchase_mutations.rs の](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/adapters/graphql/src/purchase_mutations.rs#L601-L710)[`post_purchase`](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/adapters/graphql/src/purchase_mutations.rs#L601-L710) では、GraphQL側が認可、冪等性、ロード、トランザクション、Decision、永続化を直接組み立てています。

さらに [catalog/postgres/src/create_product.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/catalog/postgres/src/create_product.rs#L51-L69) では、Postgres Adapterが `decide_create_concept` を実行しています。GraphQL側でも同じDecisionを先に実行するため、同じ検証が繰り返されています。

DMMFの思想に近づけるには、業務操作の順序をApplication Workflowという一箇所から読めるようにするのがよいと思います。

ただし、DMMFは `application` というcrateやクラス階層を要求していません。これは書籍の関数合成とI/O境界をRustで明瞭に実装するための私の提案です。

## 7. Decisionが、業務上の結果ではなくSQL更新パッチになっている

重要度：高 ／ DMMF第4章 §4.3、第5章 §5.5、第7章 §7.2〜7.3、第12章 §12.1

[policy/application/src/budget.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/policy/application/src/budget.rs#L53-L64) の `BudgetDecision` は、`name`、`amount_yen`、`starts_on`、`expected_revision`、`archived` などを多数の `Option` で保持しています。

Create・Update・Archiveの異なる結果を、DBに渡せる共通の更新データへ押し込めた形です。

このような構造は、境界の入力DTOや、DB更新のための中間表現としてなら成立します。

しかし業務上のDecisionとしては、`CreatedBudget`、`UpdatedBudget`、`ArchivedBudget` のような異なる結果を明示したほうがよいです。

```
pub enum BudgetDecision {
    Created {
        budget: ActiveBudget,
        events: Vec<BudgetEvent>,
    },
    Updated {
        budget: ActiveBudget,
        events: Vec<BudgetEvent>,
    },
    Archived {
        budget: ArchivedBudget,
        events: Vec<BudgetEvent>,
    },
}
```

これは概念コードです。実際にはそれぞれの操作で必要な情報を、意味に応じて定義します。

同様に [update_purchase_item.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/application/src/update_purchase_item.rs#L8-L25) の `quantity_set: bool` と `quantity: Option<Measure>` の組み合わせも、独立したOR型にできます。

ここでのポイントは、すべての `Option` を廃止することではありません。本当に任意の値と、操作の種類を区別するための値を混同しないことです。

## 8. Bookkeepingの仕訳モデルを、全Workflowで統一したい

重要度：高 ／ DMMF第5章 §5.4〜5.8、第6章 §6.1〜6.5

ここは、現在の実装で特に惜しいところです。

[bookkeeping/domain/src/journal.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/domain/src/journal.rs) には `BalancedJournal` があり、空でないこと、明細番号、貸借一致を構築時に保証しています。

一方、[bookkeeping/application/src/record_transfer.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/application/src/record_transfer.rs#L116-L160) は独立した `JournalLine` 型を持っています。

こちらの明細は次の3つを独立した `Option` で表現しています。

```
pub account_id: Option<AccountId>,
pub pool_id: Option<PoolId>,
pub member_id: Option<MemberId>,
```

この3つが表現する組み合わせのうち、どれが業務上正しいかは型から読み取れません。

最も美しい形を目指すなら、Bookkeeping BC内でJournalの語彙を一度統一したいです。

```
pub enum JournalTarget {
    Account(AccountId),
    Pool(PoolId),
    Member(MemberId),
}
```

ただし、これも業務モデルに応じた概念例です。対象なしが正当な明細なら、それに対応する意味のあるバリアントを用意します。

そしてPurchase、Transfer、Repayment、Correctionといった異なるWorkflowから、可能な限り共通の `BalancedJournal` を生成する設計にします。

業務上同じ概念なのに、Workflowごとに独立した不変条件を実装する状態は避けたいです。

## 9. Domain Eventと変更通知の契約を分離する

重要度：中〜高 ／ DMMF第3章 §3.2〜3.4、第7章 §7.4、第13章 §13.3.6

[PurchaseEvent](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/domain/src/purchase.rs#L384-L398) は `Posted` を表していますが、イベントの公開情報は `event_type`、`revision`、`changed_fields` など、汎用的な変更通知に近い構造です。

さらに [create_purchase.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/postgres/src/create_purchase.rs#L366-L378) では、Domain DecisionではなくPostgres側が直接 `purchase.created` のイベントレコードを構築しています。

ここはイベントの目的を明確に分けたいです。

| 種類 | 責務 |
| ------------------- | ------------------------------ |
| Domain Event | `PurchasePosted` など、業務で起きた事実 |
| Integration Event | 他BCの消費者が必要とする内容を持つ公開契約 |
| Change Notification | GraphQL Subscriptionなどに対する更新通知 |

たとえばPostgres Adapterが「購入が確定した」というイベントを勝手に決めるのではなく、Domain Workflowが事実を返し、その結果を外側がそれぞれの配送用DTOへ変換します。

ただし、すべてのDomain Eventを外部へ公開する必要はありませんし、単一BC内部にイベントバスを導入する必要もありません。

書籍の§3.4.2は、BC内部で処理の依存関係を隠すイベント配線を避けるよう勧めています。

## 10. 永続化からの復元に、Aggregateの構築経路が必要

重要度：高 ／ DMMF第5章 §5.8、第6章 §6.5、第11章 §11.4、第12章 §12.5

[post_purchase_loader.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/postgres/src/post_purchase_loader.rs#L487-L501) は、DBの値から `DraftPurchase` を直接構築しています。

個々の `Yen` やIDをSmart Constructorで復元している点は正しいです。

しかし、`DraftPurchase` 自体はフィールドを公開しており、集約全体の構造的な不変条件を保証する構築境界にはなっていません。

これにより、正しい金額・正しいIDの値を集めても、集約として正しいかどうかは次のDecisionで改めて調べる必要があります。

ここは、個別Value Objectの検証と、Aggregateの復元を分けるのがよいです。

```
impl DraftPurchase {
    pub fn reconstitute(
        // DBから復元した検証済みの要素
    ) -> Result<Self, PurchaseInvariantError> {
        // 集約全体の構造上の不変条件を保証
        todo!()
    }
}
```

なお、Draft状態に何を保証するべきかは業務上の定義次第です。未完成の購入として許される状態まで禁止するべきではありません。

また、DB復元時のエラーが、通常の業務エラーなのか、保存データ破損に相当するのかも区別するとよいです。

## 11. Evidenceの状態型も、構築可能性まで厳密にしたい

重要度：中 ／ DMMF第6章 §6.3〜6.4、第7章 §7.3、§7.9

[evidence/application/src/ingestion_job.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/evidence/application/src/ingestion_job.rs#L4-L58) は、`Queued`、`Running`、`Succeeded`、`Failed`、`Cancelled` をenumで区別しています。

これは良い設計です。

ただし `ValidLease` には公開フィールドがあり、任意のtokenや期限で生成できます。`FinishedJob.finished_at_ms` も `Option<i64>` です。

`ValidLease` という名前が保証することと、実際に構築できる値が一致していません。

期限切れになること自体は時間依存なので型だけでは防げませんが、「取得時に検証されたLease」という保証ならSmart Constructorで与えられます。

長時間実行するWorkflowについては、書籍§7.9のSaga / Process Managerに照らし、永続化する進捗と一時的な実行状態も区別すると、さらに一貫したモデルになります。

## 12. エラー型を境界まで保持する

重要度：中 ／ DMMF第10章 §10.1〜10.8

[GraphQLの](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/adapters/graphql/src/purchase_mutations.rs#L1320-L1325)[`PostMutationError`](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/adapters/graphql/src/purchase_mutations.rs#L1320-L1325) は、Workflowの失敗をペイロードのない `Workflow` バリアントにしています。

結果として、もとの `PostPurchaseError` が持つ失敗理由は境界に到達する前に失われます。

DMMFの `mapError` に対応する設計では、ステップ固有の失敗をWorkflowの意味あるエラー型へ変換し、外部公開時点で最終的なエラー契約に変換します。

公開GraphQLエラーを統合すること自体は問題ありません。重要なのは、統合が明示的な公開契約上の判断であり、内部で偶然情報を失う結果ではないことです。

## 13. Traitによる依存注入は、必要最小限の能力に絞る

重要度：改善提案 ／ DMMF第8章 §8.2、第9章 §9.6、第12章 §12.1.2

現状の `CreateAndPostPurchaseOperations` のようなWorkflow専用traitは、必ずしもDMMFへの違反ではありません。

ただし、RustではTraitを作ることが容易なため、全Workflowに広範なRepositoryやServiceインターフェースを用意する方向へ進みやすいです。

本書は、むしろ個々のI/O能力を関数として注入する方向を好んでいます。

純粋な計算なら通常の関数、振る舞いの差し替えなら `Fn`、非同期I/Oをまとめる必要がある箇所だけ局所的なTrait、という選択を推奨します。

また [household/application/src/ports.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/household/application/src/ports.rs#L24-L31) の `HouseholdStore` と、`create_household.rs` の `CreateHouseholdOperations` のように、同じ操作に近い抽象契約が複数存在する状態も整理したいです。

型やTraitの数を最大化するのではなく、必要な能力と業務上の意味を最も明瞭にすることが目的です。

## 過去のレビューから改善済みと判断できる点

リポジトリには `docs/reviews/rust_review.md`、`rust_review2.md`、`rust_review3.md` も存在しています。これらは古いコミットを対象としているため、今回の判断には最新コードを優先しました。

特に、以前に問題となっていた `bookkeeping-domain` と `bookkeeping-application` の `Purchase` / `decide_post_purchase` の二重化は、現在の構成ではDomain側への統合が進んでいます。

現在の [bookkeeping/application/src/lib.rs](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/contexts/bookkeeping/application/src/lib.rs#L26-L35) は、Domain側の型と関数を再公開しています。以前と同じ「二重実装」問題として扱うべきではありません。

また、PostPurchaseについてもGraphQL側でLoad・Decision・Persistを単一のDBトランザクション内で実行する構成が確認できるため、以前指摘されたトランザクション分離の問題をそのまま再指摘するのは適切ではありません。

今残っているのは、主として 正しいドメインモデルを、すべてのWorkflowと境界で一貫して使用すること です。

## 私なら採用する最終的な構造

現在のCargo workspaceは、すでにBC優先の方向に近づいています。完全に捨てる必要はありません。

ただし、より厳密にするなら、以下のように責務を明示します。

```
contexts/
  bookkeeping/
    domain/
      src/
        model/
          purchase.rs
          money.rs
          allocation.rs
          journal.rs
          period.rs
          settlement.rs
        workflows/
          create_purchase.rs
          post_purchase.rs
          correct_purchase.rs
          record_transfer.rs
          close_period.rs
        events.rs

    application/
      src/
        commands/
          create_purchase.rs
          post_purchase.rs
          correct_purchase.rs
          record_transfer.rs
          close_period.rs
        queries/
        capabilities.rs

    postgres/
      src/
        command_io/
        projections/
        mapping/

  household/
    domain/
    application/
    postgres/

  catalog/
    domain/
    application/
    postgres/

  policy/
    domain/
    application/
    postgres/

  evidence/
    domain/
    application/
    postgres/

integrations/
  contracts/
  anti_corruption/

read_models/
  home_summary/
  ledger/
  analytics/

adapters/
  graphql/
  http/
  worker-runtime/
  ai/
  identity/
  object-storage/

apps/
  api/
  worker/
```

これは書籍が指定するディレクトリ構造ではなく、DMMFの原則をRustのcrate可視性と依存関係で強制するための設計案です。

`policy` を `catalog` から独立させるか、独立した `read_models` ディレクトリが必要かなどは、まずContext Mapで判断します。単純にこの構造へファイルを移動するだけでは改善しません。

### 各Command Workflowの理想形

GraphQL Adapter

GraphQL Input DTO → Unvalidated Command

Application Workflow

入力検証・認可・必要なFactsの取得

Domain Workflow — Pure

Validated State + Facts → Decision + Events

Application / PostgreSQL Adapter

整合性確認・永続化・イベント記録

GraphQL Adapter

Application Result → GraphQL Output DTO

この構造で、Pure Domain WorkflowはPostgreSQLを一切知りません。GraphQLも業務操作の途中経過を知る必要がありません。

PostgreSQL Adapterがトランザクションを所有しても構いませんが、業務操作の順序と、何を判断するかはApplicationから分かる状態にしたいです。

## 修正順序

今回の目的が「移行コストより設計上の理想を優先すること」であれば、優先順位は以下のようになります。

| 順位 | 変更 | 判断 |
| -- | --------------------------------------- | ------- |
| 1 | BCの再定義、特にPolicy・Catalog・Bookkeepingの所有権 | 最初に設計判断 |
| 2 | SQL側にある業務判断をDomainへ戻す | 最優先 |
| 3 | Validated/Decisionの型保証と公開構築経路の修正 | 最優先 |
| 4 | Workflowの構成場所をApplicationに統一 | 高 |
| 5 | BudgetやPurchase編集のSQLパッチ型を業務結果型へ変更 | 高 |
| 6 | JournalモデルとAggregate復元APIを統一 | 高 |
| 7 | Domain Eventと公開イベント・通知契約を分離 | 中〜高 |
| 8 | エラー型・Trait・Evidence状態型の仕上げ | 中 |

特に1〜4は相互に関連するので、小さなリファクタリングの積み重ねよりも、一貫した設計判断のもとで改めて実装するほうが適しています。

なお、[Rust移行の決定文書](https://github.com/walnuts1018/horse/blob/b3218b75e8a0dbad28c134ccd43fda91e749ae20/docs/decisions/rust-migration.md) はGraphQL Schemaの維持を前提にしています。今回はSchema変更も許容されているので、この制約がドメインモデルを歪めるなら設計決定自体を更新してよいと思います。DMMFはGraphQL互換性を要求していません。

## 書籍への準拠と、私のRust向け提案の区別

今回の指摘のうち、Smart Constructorによる不変条件の保証、AND/OR型、状態遷移、WorkflowとI/Oの分離、BC内部モデルと公開DTOの分離、業務上のイベントを値として返すことは、書籍本文に強く根拠があります。

一方、「Workflow全体の実行制御をApplication crateへ統一する」「すべてのBCに3層のcrateを設ける」「GraphQL resolverから具体的なPostgres呼び出しを完全に排除する」は、書籍が必須としている形式ではありません。DMMFの原則をより強く守るためのRust向け設計判断です。

また、型による保証を徹底しようとして、すべての値をnewtype化したり、すべてのステップに状態型を設けたりするのも、書籍§7.10の留保を考えると必ずしも理想ではありません。

「不正な値や状態を構築できないこと」と「型の数が多いこと」は別です。

## 最終判断

現時点で最も重要なのは、`Domain`、`Application`、`Postgres` の配置を整えること自体ではありません。

業務上のDecisionを構築する時点で、その結果が型によって正当化され、外側のコードがその意味を変更できない構造にすることです。

Horseでは既に `PostPurchaseDecision` のように良い実装があります。この水準をCreatePurchase、Transfer、Budget、Policy、Evidenceなどにも一貫して広げることが、次の大きな改善だと考えます。

今回のレビューは最新ソースの主要なDomain・Application・Adapter・設定・設計文書を静的に確認したものです。181個のRustソースファイルをすべて逐行検証したわけではなく、`cargo test` やDB統合テストも実行していません。そのため網羅的な欠陥一覧ではありませんが、上記の構造的な指摘はいずれも現在のコード上で確認できるものです。

今はまだ設計を大きく変更できる段階なので、局所的な修正だけで終わらせず、BCの境界、型の構築保証、Pure Workflow、Applicationによる実行制御の4つを一貫した設計として固めることを推奨します。

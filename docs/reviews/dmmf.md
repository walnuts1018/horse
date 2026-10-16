# Horse Rust実装 — DMMF準拠性レビュー

対象：`walnuts1018/horse` · `main` · 2026年10月17日確認

結論から言うと、現在のRust実装はDMMFの基本思想をかなり取り入れています。ただし、書籍への忠実さを最優先するなら、今の段階で設計を見直したい箇所が複数あります。

特に、Bounded Contextの独立性、ドメインイベントとUI通知の混同、Workflowの責務、型による段階表現は、部分的な修正より構造を整理し直す価値があります。

今回は次の基準でレビューしています。

- DMMFへの準拠性だけを評価する。業務計算の正しさや既存Go実装との互換性は評価しない。
- 書籍に明記された原則と、Rustで忠実に実現するための私の推奨を区別する。
- 実装済みのコードで確認した問題と、今後の設計改善案を区別する。
- 実装量、変更規模、既存API・DBとの互換性は改善案の制約としない。

確認したリビジョンは [`30bf97e`](https://github.com/walnuts1018/horse/tree/30bf97e5372b34e04e76f7da15e7f3624818ffe2?utm_source=chatgpt.com) です。主にRust Workspace、各Bounded Context、Application Workflow、PostgreSQL Adapter、GraphQL境界、設計判断文書を対象にしています。

## 1. 現時点で良いところ

まず、維持すべき設計を明確にしておきます。

| 項目 | 評価 | 理由 |
| -------------------------- | --------- | --------------------- |
| BCを第一の構造単位にする | 良い | 技術レイヤー横断ではなく業務領域を軸に構成 |
| Domainを独立crateにする | 良い | フレームワーク・DBへの依存を抑制 |
| Decisionを純粋関数にする | 良い | 入力から判断結果を値として生成 |
| I/OをApplication/Adapterへ分離 | 良い | DMMF §12.1の基本方針に近い |
| NewtypeとSmart Constructor | 良いが改善余地あり | 型による制約が導入されている |
| `Purchase::Draft / Posted` | 良い | 不正な状態の組み合わせを排除 |
| Workflow別のOperations trait | 良いが改善余地あり | 汎用Repositoryより依存が局所的 |
| Workflow固有のエラー型 | 良い | Decision失敗とI/O失敗を区別 |
| GraphQLとドメイン型の分離 | 方向性は良い | 公開契約と内部モデルを区別する構造がある |

例えば、[Bookkeepingの](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/domain/src/money.rs?utm_source=chatgpt.com)[`money.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/domain/src/money.rs?utm_source=chatgpt.com) の `Yen` / `PositiveYen` は、非公開フィールドと検証付き生成を組み合わせています。これはDMMF第6章 §6.1のSmart Constructorとよく対応しています。

また、[Purchaseの状態モデル](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/domain/src/purchase.rs?utm_source=chatgpt.com#L227-L270) は次のように定義されています。

```
pub enum Purchase {
    Draft(DraftPurchase),
    Posted(PostedPurchase),
}
```

これは、複数の真偽値フラグではなく、排他的な状態を代数的データ型で表現するDMMF第7章 §7.2–7.3のアプローチです。

さらに、[CreatePurchase Workflow](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/application/src/create_purchase_workflow.rs?utm_source=chatgpt.com) は、おおむね以下の構成です。

Command

Load / Authorization

外部I/O

Pure Decision

ドメイン判断・Result

Persist

外部I/O・Transaction

これはDMMF第9章 §9.5–9.6、第12章 §12.1と整合する重要な基礎です。

したがって、今あるものを捨てて一般的なClean ArchitectureやHexagonal Architectureへ作り直す必要はありません。 DMMFとの整合性をさらに高める方向で、現在の構造を再設計するのが適切です。

## 2. 修正すべき問題 — 重要度順

重要度はDMMFへの影響度です。運用上の障害やバグの深刻度とは別です。

### P0-1. DomainがUI通知の形式を知っている

優先度 P0

これは明確に修正を推奨します。

対象：

- [`contexts/catalog/domain/src/category.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/catalog/domain/src/category.rs?utm_source=chatgpt.com)
- [`contexts/policy/domain/src/budget.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/policy/domain/src/budget.rs?utm_source=chatgpt.com)
- [`contexts/household/domain/src/mail_forwarding.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/household/domain/src/mail_forwarding.rs?utm_source=chatgpt.com)
- [`platform/household-change/src/lib.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/platform/household-change/src/lib.rs?utm_source=chatgpt.com)

現在、複数のDomain crateが `horse-household-change` に依存し、`UiChangeDescriptor` を生成しています。

例えばCatalogでは、`CategoryEvent` に `ui_change()` が存在し、`event_type`、`entity_kind`、`changed_fields` を決めています。

一方、`UiChangeDescriptor` は以下の構造です。

```
pub struct UiChangeDescriptor {
    pub event_type: &'static str,
    pub entity_kind: &'static str,
    pub changed_fields: Vec<String>,
    pub payload: serde_json::Value,
}
```

ここでの問題は、イベントを生成すること自体ではありません。

ドメインで発生した業務上の事実と、フロントエンドに変更を通知するための表現が、同じ抽象に結合されていることです。

DMMFでは、Workflowがイベントを値として生成することを推奨しています。一方で、公開メッセージやシリアライゼーションは境界側で変換するものとして扱います。

根拠：第3章 §3.4、第7章 §7.4.4、第11章 §11.2–11.4、第13章 §13.3.6。

#### 推奨する設計

Domainでは業務上の事実だけを表します。

```
pub enum CategoryEvent {
    Created {
        category_id: CategoryId,
    },
    Renamed {
        category_id: CategoryId,
    },
    Archived {
        category_id: CategoryId,
    },
    Unarchived {
        category_id: CategoryId,
    },
}
```

Applicationは、そのイベントをWorkflowの結果として受け取ります。

GraphQL Subscriptionや変更ストリームが必要なら、外側に変換を置きます。

```
pub fn to_ui_change(
    event: &CategoryEvent,
) -> UiChangeDescriptor {
    // 外部公開形式への変換
}
```

Domain Event

CategoryArchived という業務上の事実

Application / Event Mapping

必要なイベントを選択・翻訳

UI Notification DTO

GraphQL Subscription

Integration Event DTO

他のBC向け

UI向けの変更通知とBC間のIntegration Eventは、別の公開契約にできます。必ず両方を実装する必要はありません。

`UiChangeDescriptor` 自体は必要なら残して構いませんが、Domain依存から外し、例えば `adapters/change-feed` 側へ移します。

これにより、Domainから `serde_json::Value`、GraphQL風のフィールド名、フロントエンド向けの通知形式を完全に排除できます。

特に [`IssueDecision`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/household/domain/src/mail_forwarding.rs?utm_source=chatgpt.com#L46-L52) が `change: UiChangeDescriptor` を直接保持している設計は、型の意味そのものを修正したいです。

### P0-2. Workflowの処理段階が型で正確に表現されていない

優先度 P0

DMMFの中核である Make Illegal States Unrepresentable に直結する問題です。

対象：

- [`create_purchase_workflow.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/application/src/create_purchase_workflow.rs?utm_source=chatgpt.com)
- [`create_and_post_purchase.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/application/src/create_and_post_purchase.rs?utm_source=chatgpt.com)
- [`purchase_mutations.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/adapters/graphql/src/purchase_mutations.rs?utm_source=chatgpt.com#L634-L707)

具体的な証拠があります。

GraphQLから `CreatePurchaseCommand` を生成する際、配賦ルールバージョンに次の値が設定されています。

```
allocation_rule_version:
    AllocationRuleVersion::new(1)
        .expect("positive constant"),
```

各配賦にも `rule_version: 1` が設定されています。

しかし、実際の配賦ルールはWorkflowが後で `load_allocation_context()` から取得し、`apply_allocation_context()` で既存Commandを書き換えます。

つまり、まだ解決していない値を、一時的なダミー値で埋めて、解決済みの型として表現している状態です。

これはDMMFが別々の型で表現しようとしている処理段階の区別が、失われている例です。

#### 推奨する設計

未解決の入力と、配賦ルールが解決済みの入力を、別の型に分けます。

```
pub struct UnvalidatedPurchase {
    pub items: Vec<UnvalidatedPurchaseItem>,
    // 外部から受け取った未検証データ
}

pub struct ValidatedPurchase {
    // 値としての制約が検証された購入
}

pub struct AllocatedPurchase {
    // 配賦ルールと配賦結果が確定した購入
}

pub struct CreatePurchaseDecision {
    // 永続化すべき購入状態と業務イベント
}
```

ここで `AllocatedPurchase` の内部フィールドは非公開にし、正規の解決関数だけが生成できるようにします。

```
fn validate_purchase(
    input: UnvalidatedPurchase,
) -> Result<ValidatedPurchase, ValidationError>;

fn allocate_purchase(
    purchase: ValidatedPurchase,
    policy: ResolvedAllocationPolicy,
) -> Result<AllocatedPurchase, AllocationError>;

fn decide_create_purchase(
    purchase: AllocatedPurchase,
) -> Result<CreatePurchaseDecision, CreatePurchaseError>;
```

現在のモデルと理想形の違いを図示すると、次のようになります。

現状

CreatePurchaseCommand

仮のversion = 1

同じCommand

フィールドを書き換え

推奨

UnvalidatedPurchase

ValidatedPurchase

AllocatedPurchase

CreatePurchaseDecision

型ごとに何が保証されているかが明確になり、`decide_create_purchase` が未解決の配賦情報を受け取れなくなります。

DMMF第2章 §2.6.2–2.6.3、第6章 §6.3–6.4、第7章 §7.2・§7.4が、この方向性を強く支持します。

ただし、書籍第7章 §7.10は型を増やしすぎることにも留保を付けています。したがって、すべてのローカル変数に段階型を作るのではなく、現在のようにダミー値が必要になる段階を優先して型分離するのが忠実な適用です。

### P0-3. Bounded Context間の契約がPostgreSQLスキーマに依存している

優先度 P0

対象：

- [`contexts/cross-context/postgres/src/policy_settlement.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/cross-context/postgres/src/policy_settlement.rs?utm_source=chatgpt.com)
- [`contexts/bookkeeping/postgres/src/create_purchase.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/postgres/src/create_purchase.rs?utm_source=chatgpt.com)
- [`contexts/cross-context/postgres/Cargo.toml`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/cross-context/postgres/Cargo.toml?utm_source=chatgpt.com)

現在の `cross-context/postgres` は、複数のContextのPostgreSQL実装とDomain/Applicationを参照しています。

特に `policy_settlement.rs` には、`funding_schedules`、`pool_funding_rules`、`pool_funding_shares` といったテーブルへ直接アクセスする処理が置かれています。

DMMF第3章 §3.2–3.3、第12章 §12.3では、Bounded Contextは自身のモデルとデータを所有し、別のContextが内部の保存構造に依存することを避ける方針が説明されています。

現状では、業務上の意味を翻訳する契約よりも、共有PostgreSQLスキーマとその行表現がContextを結びつける傾向があります。

#### どう変えるべきか

同じPostgreSQLインスタンスを使い続けても構いません。重要なのは物理DBの独立ではなく、意味上の所有権と依存関係の独立です。

例えば、Bookkeepingが必要とするPolicyの情報を次のような契約で公開します。

```
pub struct ResolvedAllocationPolicy {
    pub version: AllocationRuleVersion,
    pub allocations: NonEmptyVec<ResolvedAllocation>,
}
```

これは概念例です。`NonEmptyVec` 自体は任意の専用型で、必須ライブラリではありません。

Policy側が、必要な情報を `ResolvedAllocationPolicy` に変換して返します。Bookkeeping側はPolicyのDB行やテーブル名を知る必要がなくなります。

特に見直すべきなのは、`cross-context` を第6の業務Contextのように扱わないことです。

次の区別が適切だと思います。

| 操作                                   | 推奨する所有者                           |
| ------------------------------------ | --------------------------------- |
| Policyのルールを判断する                      | Policy Domain                     |
| Policyのルールを読み込む                      | Policy Adapter                    |
| 購入を確定する                              | Bookkeeping Workflow              |
| 複数BCの情報を取得して翻訳する                     | Context間の明示的な契約・Adapter           |
| 複数BCのデータを組み合わせて画面に表示する               | Read-side Composition             |
| トランザクションやDependency Injectionの具体的な構築 | Composition Root / Infrastructure |

ここで注意したいのは、BCをまたぐ単一トランザクションを絶対に禁止する必要はないことです。

DMMF第6章 §6.5.3は複数集約の強い整合性について例外を認めています。ただし、これはすべてのBCにまたがる書き込みを推奨する根拠ではありません。

Horseの場合、既存の単一トランザクションを維持する必要が本当にあるなら、まず次の二者択一を検討すべきです。

1. その整合性が一つの業務モデルとして不可分なら、BCの境界自体を引き直す。
2. 別BCであることが妥当なら、翻訳契約を介した明示的な協調にし、原子性が必要な例外を設計判断として記録する。

単に `cross-context/postgres` を別の名前へ変更するだけでは解決しません。重要なのは、外部Contextの内部モデル・スキーマを知るコードを減らすことです。

### P1-4. Smart Constructorで保証した不変条件を、公開フィールドから破壊できる

これはRust固有の問題として、かなり厳密に修正したい箇所です。

対象：

- [`contexts/catalog/domain/src/category.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/catalog/domain/src/category.rs?utm_source=chatgpt.com#L4-L105)
- [`contexts/policy/domain/src/budget.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/policy/domain/src/budget.rs?utm_source=chatgpt.com#L65-L104)

`CategoryView` には `new()` があり、自分自身を親カテゴリにしないための `validate_parent()` が存在します。

しかし構造体のフィールドは公開されています。

```
pub struct CategoryView {
    pub id: CategoryId,
    pub household_id: HouseholdId,
    pub parent_id: Option<CategoryId>,
    pub name: CategoryName,
    pub status: CategoryStatus,
}
```

このため、一度有効なCategoryを生成しても、外部コードが `parent_id` を直接変更して不正な状態を作れます。

同様に、`Budget` には非公開フィールドで保護された `BudgetAmountYen` が使われていますが、`name: String` や期間のフィールドを公開しているため、Budget全体の制約が型だけで守られているわけではありません。

DMMFでは、値単位の不変条件だけでなく、複数フィールド間の不変条件も重要です。

根拠：第5章 §5.8、第6章 §6.1・§6.5。

#### 推奨

ドメイン上の不変条件を持つ型は、原則としてフィールドを非公開にします。

```
pub struct Category {
    id: CategoryId,
    parent_id: Option<CategoryId>,
    name: CategoryName,
    status: CategoryStatus,
}

impl Category {
    pub fn create(
        id: CategoryId,
        parent_id: Option<CategoryId>,
        name: CategoryName,
    ) -> Result<Self, CategoryError> {
        // 不変条件の検証
    }

    pub fn change_parent(
        self,
        parent: Option<CategoryId>,
    ) -> Result<Self, CategoryError> {
        // 遷移時の不変条件の検証
    }
}
```

一方、単なるGraphQL出力やSQLの読み取りProjectionであることが明確なら、公開フィールドのstructで構いません。

重要なのは名前と保証の一致です。

`Category` と `CategoryReadModel` を区別し、前者だけにドメイン制約の保証を持たせる設計が理想です。

これは既存の `DraftPurchase::reconstitute()` が実施している方向性とも整合します。

### P1-5. 同一Bounded Context内で、同じ業務概念の型が重複している

対象：

- [`contexts/catalog/domain/src/category.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/catalog/domain/src/category.rs?utm_source=chatgpt.com)
- [`contexts/catalog/domain/src/product_creation.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/catalog/domain/src/product_creation.rs?utm_source=chatgpt.com)
- [`contexts/policy/domain/src/create_pool.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/policy/domain/src/create_pool.rs?utm_source=chatgpt.com)
- [`contexts/policy/domain/src/set_allocation_rule.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/policy/domain/src/set_allocation_rule.rs?utm_source=chatgpt.com)

例えばCatalog内に、Category用の `ActorId` とProduct Creation用の `ActorId` がそれぞれ定義されています。

Policyでは `PoolActorId`、`AllocationActorId`、`PoolHouseholdId`、`AllocationHouseholdId` といった型が存在します。

もちろん、DMMFは「同じ基底型でも意味が違えば別の型にする」ことを推奨しています。

しかし、同じBC内で意味が同じなら、型を重複させることはむしろユビキタス言語を断片化します。

例えば、Policy Context内で意味が同一であれば、次のような語彙に統一したいです。

```
pub struct ActorId(Uuid);
pub struct HouseholdId(Uuid);
pub struct PoolId(Uuid);
pub struct AllocationRuleId(Uuid);
```

そして各Workflowから同じ型を使います。

ただし、別BCの `ActorId` や `PoolId` まで共通のグローバル型へ統一することは、必ずしも勧めません。

BC間で意味が異なる可能性を許容し、翻訳するのがDMMF第1章 §1.5、第3章 §3.3の方針だからです。

同一BC内は語彙を統一し、BC間では独立したモデルを維持するという整理がよいと思います。

### P1-6. Workflowのエラーが、Infrastructure Adapterで意味を失っている

対象：

- [`contexts/bookkeeping/application/src/workflow_failure.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/application/src/workflow_failure.rs?utm_source=chatgpt.com)
- [`contexts/cross-context/postgres/src/create_purchase_workflow.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/cross-context/postgres/src/create_purchase_workflow.rs?utm_source=chatgpt.com#L40-L99)

Domain側には `CreatePurchaseError` があり、例えば `InvalidItemCount`、`InvalidMerchant`、`DuplicatePaymentAccount` のような失敗の違いを定義しています。

ところがPostgreSQL Adapterでは、Decisionのエラーをまとめて `WorkflowRejection::InvalidInput` に変換しています。

```
CreatePurchaseWorkflowError::Decision(_) => {
    WorkflowFailure::Decision(
        WorkflowRejection::InvalidInput
    )
}
```

APIに返す段階で詳細をまとめること自体は問題ありません。セキュリティやUIの都合によって、公開したいエラーの粒度は変わります。

ただし、PostgreSQL Adapterが業務上のエラーを公開APIの粒度へ丸めるのは、責務として不自然です。

よりよい分離は以下です。

| 層 | エラーの責務 |
| ------------------ | ------------------------------ |
| Domain | `CreatePurchaseError` 等の意味ある失敗 |
| Application | Workflow固有の失敗の合成 |
| PostgreSQL Adapter | DB障害、不正な保存データ、競合等 |
| GraphQL Adapter | クライアント向けエラー契約への変換 |

DMMF第10章 §10.3–10.4の `mapError` は、単にエラーを一種類へ潰す仕組みではありません。ステップの失敗をWorkflowの契約に統合するためのものです。

Workflow固有の代数的データ型として、必要な失敗の意味を保持するのが望ましいです。

なお、現在の `WorkflowFailure<DecisionError, InfrastructureError>` 自体は悪い抽象ではありません。これをすべて廃止する必要はありません。

### P1-7. Applicationの抽象が、DMMFの関数注入よりサービス指向に寄っている箇所がある

具体例はHouseholdの作成です。

現在、次の抽象が存在します。

- [`CreateHouseholdOperations`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/household/application/src/create_household.rs?utm_source=chatgpt.com)
- [`HouseholdStore`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/household/application/src/ports.rs?utm_source=chatgpt.com)
- [`CreateHouseholdHandler`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/household/application/src/create_household_handler.rs?utm_source=chatgpt.com)

そしてPostgreSQL側には、HandlerがStoreを呼び出すためのAdapterが存在します。

これらは型として成立するものの、同じ業務操作を複数の抽象で包んでいます。

DMMF第8章 §8.2、第9章 §9.6、第12章 §12.1.2では、必要な外部能力を小さな関数として渡すアプローチが中心です。

例えば、Workflow内部に必要なのが「作成結果を保存する能力」だけなら、実質的には次の契約で十分です。

```
async fn create_household<O: CreateHouseholdOperations>(
    operations: &mut O,
    command: CreateHouseholdCommand,
) -> Result<CreatedHousehold, CreateHouseholdWorkflowError<O::Error>>;
```

GraphQL境界で動的ディスパッチが必要なら、Applicationの公開エントリーポイントに一つだけtraitを定義しても構いません。

ただし、それとは別に汎用Store、Handler、Serviceを何重にも設ける必要はありません。

個人的には、Application内部では関数と小さな能力契約を優先し、アプリ全体を結び付ける境界だけtrait objectを許容する方針に統一したいです。

一方で [`CreateAndPostPurchaseOperations`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/bookkeeping/application/src/create_and_post_purchase.rs?utm_source=chatgpt.com) は、同一トランザクションを共有する複数のI/O操作を明示しています。この構成は十分合理的であり、メソッド数が多いことだけを理由に分割する必要はありません。

### P2-8. Command、Decision、Entity、Projectionの命名が一部曖昧

これは機能上の問題ではありませんが、命名を重視するなら修正したいです。

現在は `*Command`、`*Decision`、`*Persistence`、`*Result` が多く使われています。

ただ、DMMFでは型の名前自体が、処理段階や業務上の意味を伝えるドキュメントになります。

例えば、次のように区別すると分かりやすくなります。

| 現在の傾向 | 意味に応じた推奨 |
| --------------------------- | ------------------------------------------------- |
| `CreatePurchaseCommand` | `UnvalidatedPurchase` または `CreatePurchaseCommand` |
| 解決済みの同一Command | `AllocatedPurchase` |
| `CreatePurchaseDecision` | 保存対象を意味する `DecidedPurchase` 等 |
| `CreatedPurchase` | 永続化成功を表す `CreatePurchaseReceipt` |
| `PurchaseAllocationContext` | `ResolvedAllocationPolicy` 等 |
| `CategoryView` | `Category` または `CategoryReadModel` |
| `CreateHouseholdPortError` | 実際の失敗責務に対応するWorkflow Error |

これらは機械的な一括リネーム案ではありません。

特に `Decision` という名前は、ドメイン判断を表す型として有用です。しかし、単なる検証済みデータを表すのか、状態遷移とイベントを含む結果なのかが型の名前から見えなくなるほど一律に使う必要はありません。

DMMF第2章 §2.4–2.6、第5章 §5.5、第7章 §7.2が、型シグネチャを設計上のドキュメントとして扱う根拠になります。

## 3. さらに美しくするなら：アーキテクチャ全体の再構成

ここからは、個別の問題を直すだけでなく、DMMFにもっと忠実な構造へ整えるための提案です。

### 3.1 `cross-context` を業務ContextではなくCompositionの概念へ整理する

現在の `contexts/cross-context` は、業務上のBounded Contextというより複数のBCを結び付ける技術的・アプリケーション的な構成要素です。

そのため、`contexts/` の直下に並べると、あたかも同格のBCであるかのように見えます。

私なら、次のように分割します。

- 状態変更Workflowは、原則として業務上の状態所有者のBCへ置く。
- 複数BCのQueryを組み合わせる処理は、Read-side Compositionへ置く。
- BC間の翻訳は、明示的な契約を実装するAdapterへ置く。
- 技術的な依存関係の構築は、Composition Rootへ置く。

例えば、現在の `cross-context/application/settlement_workflow.rs` は、期間を締めるという操作の所有者がBookkeepingなら、Bookkeeping側に属するべきです。

逆に、複数Contextからデータを集めてホーム画面を組み立てる処理は、Read-sideのCompositionとして扱えます。

この区別はDMMF第3章 §3.4の「Workflowは一つのBCに属する」という原則と、第12章 §12.2のCQRSに沿っています。

### 3.2 読み取りモデルをBCの内部状態と混同しない

例えば [`household/postgres/src/create_household.rs`](https://github.com/walnuts1018/horse/blob/30bf97e5372b34e04e76f7da15e7f3624818ffe2/contexts/household/postgres/src/create_household.rs?utm_source=chatgpt.com) の `load_household_inner` は、Householdだけでなく、Account、Category、Poolなどのデータも直接取得して、画面向けの読み取りモデルへ組み立てています。

これはCQRSの読み取り処理としては十分あり得る実装です。

しかし、Household BCがBookkeeping、Catalog、Policyのデータモデルをすべて所有しているように見えてしまいます。

より明確にするなら、`HouseholdOverviewQuery` のような読み取り側のCompositionへ移し、各BCの公開Projectionから組み立てます。

ここで重要なのは、読み取り専用のProjectionにまでSmart Constructorや厳密なAggregate構築を強制しないことです。

DMMF第12章 §12.2は、Query側とCommand側で異なるモデルを使うことを認めています。

### 3.3 推奨するディレクトリ構造

現在のWorkspace構造を土台に、私なら次のように整理します。

horse/

```
apps/
  api/
  worker/

contexts/
  bookkeeping/
    domain/
      src/
        model/
        workflows/
        events.rs
    application/
      src/
        workflows/
        queries/
        contracts/
    postgres/
      src/
        persistence/
        queries/
        mappings/

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

compositions/
  queries/
    household_overview/
    financial_dashboard/

adapters/
  graphql/
  http/
  identity/
  ai/
  object_storage/
  worker_runtime/
  change_feed/

platform/
  postgres_common/
  observability/
  authorization/

docs/
  domain/
    context-map.md
    ubiquitous-language.md
    workflows/
    decisions/
```

これはRust Cargo構成を完全に変更しなければいけないという意味ではありません。現在の `domain` / `application` / `postgres` のcrate分割は有用なので、基本的に維持して構いません。

ただし、`domain/src/workflows/` は純粋なDomain Decisionを、`application/src/workflows/` はI/Oの実行順序を、それぞれ担当するものとします。

名前が同じWorkflowであっても、意味の違うコードを明確に分けられます。

さらに、`contracts/` は巨大な共通インターフェース置き場ではなく、必要な境界契約を限定的に公開する用途にします。

### 3.4 型の責務を4種類に明確化する

個々の型を、次の4種類のどれなのかで整理したいです。

| 種類 | 不変条件 | 例 |
| ----------------------- | --------- | ------------------------------------------- |
| Untrusted Input / DTO | 未保証 | `UnvalidatedPurchase` |
| Domain Value / State | 構築時に保証 | `AllocatedPurchase`, `OpenAccountingPeriod` |
| Decision / Domain Event | 判断結果として保証 | `PostPurchaseDecision`, `PurchasePosted` |
| Read Model / Projection | 読み取り用途の契約 | `PurchaseSummary`, `HouseholdOverview` |

これにより、`pub` フィールドを禁止するか、`Serialize` / `Deserialize` を付けてよいか、Smart Constructorを必須にするか、といった判断が明確になります。

特に永続化からDomainへ戻す処理は、単なる `From<DbRow>` よりも、不正な永続値を明示できる `TryFrom<DbRow>` や専用の `reconstitute` 関数を優先したいです。

これはDMMF第11章 §11.4、第12章 §12.5の直接的な適用です。

## 4. DMMFを厳密に守るために、逆にやりすぎない方がよいこと

ここはかなり重要です。

DMMFへの忠実さを追求すると、あらゆる概念を型にし、すべての外部操作をtraitにし、すべての状態変更をイベント駆動にしたくなるかもしれません。

しかし、それが常にDMMFに忠実とは限りません。

| やりすぎの例 | 書籍に沿った判断 |
| ------------------------------ | ------------------- |
| すべての文字列をnewtype化 | 業務的に区別すべき意味や不変条件を優先 |
| すべてのWorkflowをasyncにする | I/Oを含む部分と純粋な部分を分ける |
| すべてのI/OにRepository traitを作る | 個別の小さな能力を注入する |
| BC間は必ずメッセージング | 業務上の境界と整合性要件で判断 |
| BC内でもイベントバスで連結する | BC内部は明示的な関数合成を優先 |
| すべてのQueryでDomain Aggregateを再構築 | CQRSの読み取りモデルを活用 |
| すべての失敗をドメインエラーにする | 業務失敗とインフラ障害・バグを区別 |
| Event Sourcingを導入する | 必要性があれば別途判断 |

特に現在のHorseが既に採用している、純粋なDecisionとI/Oの分離は有用です。これを過度に抽象化して見通しを悪くするより、今ある明確な処理の流れを維持するべきです。

この点は、書籍分析の第7章 §7.10、第9章 §9.6、第12章 §12.1.2と整合します。

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

DMMF\_%E6%9B%B8%E7%B1%8D%E5%85%A8%E7%AB%A0%E5%88%86%E6%9E%90\_%E8%A8%AD%E8%A8%88%E8%AD%B0%E8%AB%96%E7%94%A8.md

## 5. 修正するなら、この順番を推奨する

個々の問題を別々に直すより、概念を整理する順序を意識したほうが、最終的な設計が美しくなります。

1. BCの責務と所有権を確定する

   `Household`、`Policy`、`Catalog`、`Bookkeeping`、`Evidence` のContext Mapを定義し、Command・Query・業務データの所有者を明記する。
2. DomainからUI契約への依存を除去する

   `UiChangeDescriptor`をDomainから排除し、必要ならDomain Eventを純粋なenumとして定義する。
3. Workflowの段階型を正確にする

   特に購入作成で使われているダミーの配賦ルールバージョンをなくし、未解決・解決済みの型を区別する。
4. 型の不変条件を監査する

   Domainの公開フィールド、Smart Constructorの迂回経路、永続化からの再構築、同一BC内の重複newtypeを整理する。
5. Applicationの抽象と命名を整理する

   Workflowの処理順序を明示し、不要なHandler/Storeの重複を減らして、エラーの意味を境界まで保持する。
6. Context横断のQueryと技術的Compositionを再配置する

   `cross-context`を業務Contextとして扱わず、Query CompositionとBC間Adapterへ責務を分配する。
7. 設計上の制約を自動検証する

   Domain crateに許可する依存を限定し、DomainからUI通知・GraphQL・SQL・DTOの型が漏れないようにする。Domainの構築経路や状態遷移を単体テストで検証する。

ここで最初のContext Mapは、単なるディレクトリ対応表にしないことが大切です。

各BCについて、ユビキタス言語、所有する状態、公開する能力、依存する他BCの情報、整合性境界を記録します。

それを基準にコードを再配置します。コード上の依存関係を見てからBCの名前を後付けするのではなく、業務上の境界を起点にするのがDMMF第1〜3章の基本思想です。

## 6. 最終的な評価

現状のRust実装は、「DMMFとは別方向のアーキテクチャになっている」という状態ではありません。 むしろ、根幹のアプローチはかなり一致しています。

特にBookkeepingの型付き状態とPure Decisionは、今後の設計の基盤として維持する価値があります。

ただし、DMMFをできる限り厳密に適用するという目標に対しては、次の3つの改善を特に重視します。

第一に、Domainを本当に業務上の意味だけで構成すること。 UI通知、GraphQLのフィールド名、永続化上の構造をDomainの関心事から切り離したいです。

第二に、Workflowの処理段階を型として表現すること。 ダミー値や変更可能なCommandで段階の違いを隠すのではなく、正しく解決・検証された値だけを次の関数が受け取るようにしたいです。

第三に、Bounded Contextの独立性をコード構造だけでなく契約にも反映すること。 `cross-context/postgres` の存在が即座に間違いというわけではありませんが、現在は他BCのテーブル構造を知るコードが多いため、Context間契約の設計を強化する余地があります。

なお、今回のレビューはGitHub上の静的なソース確認であり、Cargoのコンパイル・テスト実行や全Rustファイルの逐行監査までは行っていません。したがって、挙げた問題は具体的なコードで確認できたものですが、全違反を網羅したという評価ではありません。

最も優先すべきなのは、今すぐ抽象やレイヤーを増やすことではなく、BC・Domain Event・Workflowの型契約を改めて定義し、その契約にコード構造を揃えることです。 それが、DMMFの設計思想に沿いながら、Rustとしても読みやすく、堅牢な実装へ近づける道筋だと思います。

# Horse Rust実装のDMMF準拠レビュー

構造的な改善を推奨

結論として、現在のHorseはDMMFの方向へかなり進んでいますが、まだ「DMMFの設計原則がアーキテクチャ全体で強制されている状態」ではありません。

特にBookkeepingの一部は非常に良い方向に進んでいます。一方、ほかのBounded Contextでは、純粋なDecision関数を導入したものの、ドメインモデル自体はプリミティブと公開フィールドを中心とする設計にとどまっています。

最も大きな改善余地は、単純なレイヤーの追加ではなく、次の4つです。

1. 型の正しさを、Workflowの一部分だけでなくBC全体で保証する
2. Workflowを業務操作の唯一の入口にし、GraphQLとPostgreSQLから業務判断を追い出す
3. Bounded Contextの境界を、ディレクトリだけでなくデータ所有・契約・依存関係でも成立させる
4. Decision・永続化・イベント・エラーの意味が、境界を越えても失われないようにする

さらに、トランザクション境界には優先して修正すべき具体的な問題があります。後述します。

## レビュー対象

2026年10月16日の `main`、コミット [`784f4cbd`](https://github.com/walnuts1018/horse/tree/784f4cbd152cbc5d139e5404ac4e0089b7f9745c) を確認しました。

Rust Workspaceの構成、主要なDomain/Application/PostgreSQL/GraphQL実装、Workflow、イベント、状態型、設計決定文書を調査し、提供されているDMMF全13章の分析結果と照合しています。巨大なコードベースの全行検査や実行テストを完了したという意味ではありません。

評価対象はドメインロジックの計算結果の正しさではなく、その正しさを型・関数・境界で表現できているかです。

また、以下では区別を明確にします。

- DMMFの原則：書籍本文に明確な根拠があるもの
- Rustへの適用判断：原則をRustで強く実現するための私の提案
- 実装上の問題：リポジトリに具体的な証拠があるもの

## 1. 現在の設計で維持したい部分

### Pure DecisionとI/Oの分離

例えば [`contexts/bookkeeping/domain/src/purchase.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/bookkeeping/domain/src/purchase.rs#L629) の `decide_post_purchase` は、Commandを受け取って純粋に `PostPurchaseDecision` または `PostPurchaseError` を返します。

`PostPurchaseDecision` は、確定後の購入、仕訳、負担スナップショット、イベントをまとめて保持しています。

これはDMMF第7章 §7.4、第9章 §9.6、第12章 §12.1に沿った優れた設計です。DB更新を含む関数から結果を返しているのではなく、何を実行すべきかという決定をデータとして表現しているからです。

### Smart Constructorと再構築

[`money.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/bookkeeping/domain/src/money.rs) の `Yen`、`PositiveYen` は非公開フィールドと `Result` を使っています。

[`purchase.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/bookkeeping/domain/src/purchase.rs#L92) の `DraftPurchase::reconstitute` は集約の内部整合性を検証します。また、[`post_purchase_loader.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/bookkeeping/postgres/src/post_purchase_loader.rs#L470) はDBレコードからのドメイン再構築にこれを利用しています。

これは第6章 §6.1、第11章 §11.4、第12章 §12.5が求める重要な性質です。

### AND/OR型と状態モデリング

`Purchase::Draft | Posted`、`PostingTarget::Pool | Account`、`ExtractionJob::Queued | Running | Succeeded | Failed | Cancelled` など、状態と排他的な選択をenumとして表しています。

特に [`ingestion_job.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/evidence/application/src/ingestion_job.rs#L50) で、実行中ジョブが `ValidLease` を持つモデルは、型による制約の表現として良いです。

### Workflowの手順が明示的

[`create_and_post_purchase.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/bookkeeping/application/src/create_and_post_purchase.rs#L171) は、外部情報を取得し、純粋なDecisionを呼び、永続化して、後続のDecisionへ進む流れを明示しています。

BC内で暗黙的なイベントバスを使って工程をつないでいない点も、第3章 §3.4.2に沿っています。

また、`trait` を使ったOperations注入そのものは問題ありません。DMMFはF#の関数注入を説明していますが、Rustで狭いOperations traitを使うことは原則と矛盾しません。

これらは維持すべき設計資産です。全面的に捨てて作り直す必要はありません。

## 2. 修正事項の全体像

私の評価は以下です。

| 優先度 | 項目 | 判定 |
| --- | --------------------------- | ----------- |
| P1 | ドメイン型・Smart Constructorの一貫性 | 未解決箇所が残る |
| P1 | Bounded Contextの所有権・境界 | 再設計が必要 |
| P1 | 不正状態・不正遷移の型表現 | 改善余地がある |
| P2 | エラー型と境界変換 | 型の意味を保持したい |
| P2 | DomainとApplicationの責務 | 整理が必要 |
| P2 | Operations traitと依存性注入 | さらに洗練できる |
| P2 | アーキテクチャの自動検証 | 導入したい |

残っているP1を優先して修正します。P2には、DMMFが直接禁止しているわけではなく、Rust上でより忠実に表現するための改善も含めています。

## 3. P1：Domain ModelがBCによって著しく違う

これは今回のレビューで最も重要な、純粋なDMMFの問題だと考えています。

### Bookkeepingでは型が意味を持っている

例えば次の型があります。

```
pub struct Yen(i64);
pub struct PositiveYen(Yen);
pub struct PurchaseId(String);
pub struct AccountId(String);
pub struct DraftPurchase { /* private fields */ }
```

それぞれ意味を区別し、重要な不変条件を構築時に検査しています。

Bookkeepingにも[`create_purchase.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/bookkeeping/application/src/create_purchase.rs#L83-L211)のように`String`や`i64`を公開するDecisionが残っています。各Bounded Contextについて、識別子の取り違え、不変条件、検証済み状態、業務上の意味を型として保証できる値を引き続き確認する必要があります。

DMMFの第4章 §4.3、第5章 §5.3、第6章 §6.1、第7章 §7.2に対応する改善です。

ただし、すべてのStringを無条件にnewtypeへ変えることまでは推奨しません。 第7章 §7.10では型の増やしすぎへの留保もあります。

型を増やす基準は、識別子の取り違え、不変条件、検証済み状態、業務上の意味を静的に保証できるかどうかです。

## 4. P1：不正な状態がまだ型として表現可能

Bookkeepingの中でも、より強くDMMFへ寄せられます。

### AccountingPeriodの状態型

AccountingPeriodは `OpenPeriod` と `ClosedPeriod` を分離する余地があります。これは現在のenumが誤りという意味ではなく、許可される操作を状態型で制約できるという改善です。

根拠はDMMF第6章 §6.3–6.4、第7章 §7.2–7.3です。

## 5. P1：Bounded Contextの境界がディレクトリ上の区別にとどまっている

現在の構成は、

```
contexts/
  bookkeeping/
  catalog/
  household/
  policy/
  evidence/
  cross-context/
```

となっています。

BCを主軸にしたこと自体は良いです。しかし、技術的な依存関係を見ると、まだBCとしての自律性が十分ではありません。

### cross-context/postgresに更新処理が存在する

[`cross-context/postgres/src/pool_mutations.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/cross-context/postgres/src/pool_mutations.rs#L30-L155) は、Poolの作成に加えてAllocation Defaultsの更新、Householdのrevision更新、イベント保存まで実行しています。

これは「BCをまたぐ処理だからcross-contextに置く」という技術的な分割になっています。

問題は、その操作が最終的にどのBCの責任なのかが曖昧になることです。

### 私ならこう整理します

Household、Catalog、Policy、Bookkeepingの境界を一度再検討します。

特にPool、Allocation Rule、Funding Rule、Household Defaultsについて、誰が業務ルールと状態変更の所有者なのかを明確にする必要があります。

例えば、Allocation RuleがBookkeepingにとって単なる参照データではなく、購入確定時の整合性を強く支配する概念であるなら、Policyを独立BCとして扱うこと自体を見直す価値があります。

逆に独立BCとするなら、Bookkeepingは公開された契約やローカルなスナップショットを使って参照すべきです。

`cross-context` は、原則としてQuery用のRead Modelや、明示的なBC間調整に限定したいです。

なお、DMMFは跨BC処理のすべてを非同期化せよとは言っていません。 既存の強い原子性が業務上必要なら、まずBC境界の切り方を再検討すべきです。分散イベントに置き換えることが目的ではありません。

これは小さなファイル移動ではなく、設計上の重要な意思決定です。

## 6. P2：Errorの意味が境界を越えると弱くなる

またGraphQL側では、PostgreSQL Adapterの詳細なエラー型を直接matchしている場所があります。

これはドメイン上の失敗と、永続化実装上の失敗の変換責務が明確ではない状態です。

### 推奨する分離

```
pub enum CreateHouseholdError {
    InvalidName,
    InvalidMembers,
    PermissionDenied,
    AlreadyExists,
}

pub enum WorkflowFailure<E> {
    Domain(CreateHouseholdError),
    Infrastructure(E),
}
```

最終的なGraphQL変換で、業務上のエラーを公開UserErrorに変換し、Infrastructureの障害は適切に内部エラーとして扱うとよいです。

DMMF第10章 §10.1–10.5は、すべてのエラーをひとつの型に統一することを推奨していません。

重要なのは、業務上意味のある失敗を型で表し、必要な段階まで意味を保持することです。

## 7. P2：DomainとApplicationの責務をもう一段明確にしたい

現在は `bookkeeping/domain` と `bookkeeping/application` に純粋なロジックが分散しています。

例えば、

- `bookkeeping/domain/purchase.rs` に `decide_post_purchase`
- `bookkeeping/application/create_purchase.rs` に `decide_create_purchase`
- `bookkeeping/application/correct_purchase.rs` に `decide_correct_purchase`
- `bookkeeping/application/close_period.rs` に `decide_close_period`

があります。

純粋関数がApplicationに存在すること自体はDMMF違反ではありません。

しかし、今回のようにDomainとApplicationを明確に分割するなら、次の区別を徹底したいです。

Domain

業務上の型、不変条件、集約、純粋な意思決定、状態遷移、業務イベント。

Application

公開Commandの処理、外部事実の取得、Domain Functionの合成、永続化や通知の実行順序。

例えば `decide_correct_purchase` は業務判断としてDomain側へ移し、`correct_purchase` WorkflowはApplication側に残す形です。

これはDMMFそのものが要求するクレート分割ではなく、Rust上で責務をわかりやすくするための提案です。

また、Bookkeepingの `DraftPurchase::update_allocation_funding(&mut self, ...)` のような操作も、関数型モデルへ厳密に寄せるなら、値を消費して新しい状態を返すAPIへ変更する余地があります。

一方、純粋関数の内部でローカル変数やVecを変更することまで禁止する必要はありません。

## 8. P2：Operations traitをもっと局所的なCapabilityとして考える

[`create_and_post_purchase.rs`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/contexts/bookkeeping/application/src/create_and_post_purchase.rs#L123-L169) の `CreateAndPostPurchaseOperations` は、複数のI/O操作を定義しています。

この実装には利点があります。順序が見え、依存関係をモック化でき、DBトランザクションを維持しやすいことです。

したがって、Operations traitを使っていることを理由に全面変更する必要はありません。

ただ、DMMF第9章 §9.5–9.6、第12章 §12.1.2が強調するのは、小さなI/O関数を必要な関数に注入することです。

理想的には、各ステップが依存するCapabilityをより局所化できます。

```
LoadAllocationFacts
RequireActiveMember
LoadDraftPurchase
PersistPurchaseDecision
```

実装方法としては関数、クロージャ、狭いtraitのいずれでも構いません。

`trait Repository` を多数作るより、業務操作が実際に必要とする能力を名前付きで表すことのほうが重要です。

## 9. 私が推奨する最終的なディレクトリ構造

変更コストを無視して理想形を追うなら、現行の構造を少し整理して次の方向へ進めたいです。

これは本書が指定する公式構造ではなく、今回のHorseに対するRust上の適用案です。

Rust Workspace構成案

```
apps/
  api/
  worker/

contexts/
  bookkeeping/
    domain/
      src/
        values/
        aggregates/
        decisions/
        events/
        errors/
    application/
      src/
        workflows/
          create_purchase/
          post_purchase/
          correct_purchase/
          close_period/
        queries/
    postgres/
      src/
        loaders/
        persistence/
        projections/

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

  evidence/
    domain/
    application/
    postgres/

integrations/
  read_models/
  process_managers/

adapters/
  graphql/
  http/
  worker_runtime/
  ai/
  identity/
  object_storage/

platform/
  postgres_common/
  observability/
  test_infra/
```

ただし、以下の点が重要です。

まずBounded Contextの境界を確定してから構造を作るべきです。 このツリーは現行BCを仮に維持した整理例であり、最終的なContext Mapの決定ではありません。

`integrations/read_models` は跨BCのQuery Projectionを置く候補です。`process_managers` は本当にBC間の長時間ビジネスプロセスが必要な場合だけ作ります。すべての複合処理をここへ移すべきではありません。

また、`domain/values` や `domain/aggregates` も、細分化すること自体に価値があるわけではありません。ビジネス概念が十分に大きくなった場合に分ければよいです。

DMMFらしさの本質はディレクトリ数ではなく、次の依存構造にあります。

目指す依存方向と実行フロー

これは論理上の呼び出しフローです。コードの依存方向はDomainを内側とし、InfrastructureからApplicationのI/O契約を実装します。DomainはDBにもGraphQLにも依存しません。

特に `adapters/graphql` が複数の `*-postgres` と直接結合する現在の構成は、理想形ではWorkflowの呼び出し口とQuery Projectionの公開口へ整理したいです。

特にSmart Constructorは、後からフィールドや `Default` 実装が追加されたときに不正な構築経路が生まれないことまで確認したいです。

## 10. 変更する順番

私なら、今回の改善を次の順番で行います。

1

トランザクションの失敗伝播を統一

Workflowの失敗が確実にRollbackへ伝播する構造へ変更し、統合テストで固定する。

2

Context Mapとデータ所有権を再定義

特にHousehold・Catalog・Policy・Bookkeepingの間で、どのBCが何を所有し、何を公開するのか決め直す。

3

Domain型の設計を全BCで統一

ID、値オブジェクト、Smart Constructor、集約、状態型を整備する。未検証入力と検証済み状態を明確に区別する。

4

Workflowの入口を統一

GraphQLとPostgreSQLが直接Decisionを呼ぶ形から、Application WorkflowがLoad・Decide・Persistを所有する形に変更する。

5

DTO・イベント・エラーの境界を完成させる

Domainの意味を保ちながら、DB Row・GraphQL型・更新通知・外部イベントへ明示的に変換する。

6

設計制約をコンパイル時・CIで強制

以後、実装の追加によってDMMFの原則が徐々に崩れていかないようにする。

## 11. 今の設計方針文書も更新したほうがよい

[`docs/decisions/rust.md`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/docs/decisions/rust.md) は、今回の実装よりも厳密なDMMF寄りの方針をすでに表明しています。

特に、Domain型、Workflow、DTO分離、狭いCapability、純粋なDecisionという設計理念は良いです。

しかし、コードがその宣言にまだ完全には追いついていません。

また [`rust-migration.md`](https://github.com/walnuts1018/horse/blob/784f4cbd152cbc5d139e5404ac4e0089b7f9745c/docs/decisions/rust-migration.md) は既存GraphQL Schemaの維持を決定事項としています。

今回の前提では移行互換性を考慮する必要がありません。したがって、GraphQL SchemaやDB Schemaを既存契約へ合わせる制約は設計方針から取り除いてよいと思います。

ただし、GraphQLを変更すること自体がDMMFへの忠実さを高めるわけではありません。ドメインモデルの表現を優先し、そのうえで外部契約を独立して設計することに意味があります。

## 最終評価

現在のHorseは、DMMFの考え方を表面的に取り入れているだけの実装ではありません。

特に `PostPurchaseDecision`、`DraftPurchase::reconstitute`、`BalancedJournal`、`ExtractionJob` などには、書籍の型駆動・関数型モデリングがきちんと反映されています。

一方で、まだ次の状態が混在しています。

| 目指すDMMFの状態 | 現在の実装 |
| ------------------- | ------------------------------------- |
| 意味を保証するDomain型 | 一部BCではStringと公開structが中心 |
| BCが内部モデルを所有 | cross-contextに他Context所有データを更新する処理が残る |
| 型によって不正状態を排除 | 訂正対象の状態を含む状態フラグが残る |

いちばん優先したいのは、「Pure Decisionを増やす」ことではなく、「そのDecisionが受け取る値と返す値が、型として意味を保証する」状態へ進めることです。

これがDMMF第4〜7章の中核であり、その後のWorkflow合成、I/O分離、DTO変換、進化可能性を成立させる前提です。

そしてBounded Contextと実行境界が正しく確立できれば、現行構造にある良い設計を保持しつつ、かなり一貫した関数型ドメインモデルへ進化させられると考えています。

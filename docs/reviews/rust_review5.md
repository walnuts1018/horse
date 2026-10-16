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

特に[`ingestion_job.rs`](../../contexts/evidence/domain/src/ingestion_job.rs)で、実行中ジョブが`ValidLease`を持つモデルは、型による制約の表現として良いです。

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
| P2 | エラー型と境界変換 | 型の意味を保持したい |

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

各Bounded Contextについて、識別子の取り違え、不変条件、検証済み状態、業務上の意味を型として保証できる値を引き続き確認する必要があります。

DMMFの第4章 §4.3、第5章 §5.3、第6章 §6.1、第7章 §7.2に対応する改善です。

ただし、すべてのStringを無条件にnewtypeへ変えることまでは推奨しません。 第7章 §7.10では型の増やしすぎへの留保もあります。

型を増やす基準は、識別子の取り違え、不変条件、検証済み状態、業務上の意味を静的に保証できるかどうかです。

## 4. P1：Bounded Contextの境界がディレクトリ上の区別にとどまっている

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

## 5. P2：Errorの意味が境界を越えると弱くなる

口座作成、カード番号下4桁更新、入金記録、振替記録では、PostgreSQL AdapterのエラーをApplicationの`WorkflowFailure`へ分類してからGraphQLへ返すようにしました。GraphQLは`Decision`、`NotFound`、`Conflict`、`Infrastructure`だけを扱い、PostgreSQL固有の列挙子を分岐しません。

口座関連の明細紐付け処理では、PostgreSQL AdapterのエラーをApplication Workflow境界で`WorkflowFailure`へ分類し、GraphQL Adapterは`Decision`、`NotFound`、`Conflict`、`Infrastructure`だけを扱うようにしました。ほかのBounded Contextには同じ依存が残っているため、この項目全体は未解決です。

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

## 6. Workspace構造と依存境界

Bounded Context間のデータ所有権は未解決です。特にPool、Allocation Rule、Funding Rule、Household Defaultsの業務ルールと状態変更をどのContextが所有するかを決め、`cross-context/postgres`の更新処理が各Contextの責任と一致するよう整理する必要があります。

`adapters/graphql`が複数の`*-postgres`へ直接依存している境界も未解決です。GraphQLからApplication Workflowを呼び出し、Query Projectionを公開する依存構造へ整理する必要があります。DomainはDBとGraphQLに依存させません。

## 7. 変更する順番

私なら、今回の改善を次の順番で行います。

1

Context Mapとデータ所有権を再定義

特にHousehold・Catalog・Policy・Bookkeepingの間で、どのBCが何を所有し、何を公開するのか決め直す。

2

Domain型の設計を全BCで統一

ID、値オブジェクト、Smart Constructor、集約、状態型を整備する。未検証入力と検証済み状態を明確に区別する。

3

Workflowの入口を統一

GraphQLとPostgreSQLが直接Decisionを呼ぶ形から、Application WorkflowがLoad・Decide・Persistを所有する形に変更する。

4

DTO・イベント・エラーの境界を完成させる

Domainの意味を保ちながら、DB Row・GraphQL型・更新通知・外部イベントへ明示的に変換する。

## 最終評価

現在のHorseは、DMMFの考え方を表面的に取り入れているだけの実装ではありません。

特に `PostPurchaseDecision`、`DraftPurchase::reconstitute`、`BalancedJournal`、`ExtractionJob` などには、書籍の型駆動・関数型モデリングがきちんと反映されています。

一方で、まだ次の状態が混在しています。

| 目指すDMMFの状態 | 現在の実装 |
| ------------------- | ------------------------------------- |
| 意味を保証するDomain型 | 一部BCではStringと公開structが中心 |
| BCが内部モデルを所有 | cross-contextに他Context所有データを更新する処理が残る |

いちばん優先したいのは、「Pure Decisionを増やす」ことではなく、「そのDecisionが受け取る値と返す値が、型として意味を保証する」状態へ進めることです。

これがDMMF第4〜7章の中核であり、その後のWorkflow合成、I/O分離、DTO変換、進化可能性を成立させる前提です。

そしてBounded Contextと実行境界が正しく確立できれば、現行構造にある良い設計を保持しつつ、かなり一貫した関数型ドメインモデルへ進化させられると考えています。

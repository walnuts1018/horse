# Horse の Rust 実装レビュー（2026年10月15日時点）

結論：DMMF に沿った方向性は良いですが、今のうちに修正したい問題がいくつかあります。 特に、月次締め・訂正の状態遷移、Workflow とトランザクションの結合方法、ドメイン型の配置は、実装が増える前に手を入れる価値があります。

GitHub の `main` ブランチ、コミット [`e8565c4`](https://github.com/walnuts1018/horse/commit/e8565c4cb5afd0687be65bd4e65e57d8518446d4) を対象に、Rust の Domain、Workflow、PostgreSQL Adapter、Migration、設計文書を確認しました。

DB ライブラリの Diesel 2.3 + `diesel-async` 0.9.2、Migration 管理の Atlas は問題ありません。 SQLx を使わないこと自体は DMMF からの逸脱ではありません。

## まず修正したい問題

確認結果

コードと設計の静的レビュー

# 4

具体的な不整合・欠陥

# 3

実装前に直したい設計リスク

# 1

認可境界の注意点

まだ Rust API は未接続のため、実運用上の脆弱性と断定するものではありません。Cargo のビルド・統合テストは実行していません。

## 1. 月次締め後の購入を訂正できない

重要度：高・実際に矛盾する状態遷移

これは明確な問題です。

`close_period.rs` は、締め対象の購入を `POSTED` から `CLOSED` に更新しています。

[postgres/src/close_period.rs L174–193](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/close_period.rs#L174-L193)

```
.set((
    purchases::status.eq("CLOSED"),
    purchases::revision.eq(next),
    purchases::updated_at.eq(now),
))
```

しかし、訂正の純粋関数は次のように検証しています。

[bookkeeping-workflows/src/correct_purchase.rs L216–218](https://github.com/walnuts1018/horse/blob/e8565c4/crates/bookkeeping-workflows/src/correct_purchase.rs#L216-L218)

```
if command.purchase_status != PurchaseStatus::Posted {
    return Err(CorrectPurchaseError::PurchaseNotPosted);
}
```

さらに PostgreSQL Adapter でも `POSTED` 以外を拒否しています。

[postgres/src/correct_purchase.rs L94–103](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/correct_purchase.rs#L94-L103)

つまり、締め処理を実行すると、その購入に対する訂正ができなくなります。

一方、Horse の `docs/plan.md` では「締め後は過去を直接編集せず、訂正仕訳や調整取引を追加する」ことを明示しています。

修正案

私なら `PurchaseStatus::Closed` を単純に許可するだけではなく、そもそも Purchase のライフサイクルと AccountingPeriod のライフサイクルを分けます。

```
pub enum Purchase {
    Draft(DraftPurchase),
    Posted(PostedPurchase),
}

pub enum AccountingPeriod {
    Open(OpenPeriod),
    Closed(ClosedPeriod),
}
```

締め済みの購入も `PostedPurchase` のままです。

通常編集は会計期間によって制限しますが、訂正仕訳を追加する Workflow は締め済みの購入を参照できます。

これは DMMF 第7章の「異なる状態軸を混ぜない」考え方により近い設計です。

## 2. `lib.rs` に登録されていない Rust ファイルが存在する

重要度：高・ビルド検証漏れ

Rust の module 宣言が不足しています。

`crates/bookkeeping-workflows/src/` には、

- `create_account.rs`
- `create_household.rs`
- `create_pool.rs`

が存在しますが、[bookkeeping-workflows/src/lib.rs](https://github.com/walnuts1018/horse/blob/e8565c4/crates/bookkeeping-workflows/src/lib.rs#L1-L14) にはそれらの `pub mod` 宣言がありません。

同様に、[postgres/src/lib.rs](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/lib.rs#L1-L16) には `create_account`、`create_household`、`create_pool`、`account_balance_analysis` などの宣言がありません。

Rust は、`src/` に `.rs` ファイルを追加しただけでは、そのファイルを自動的にコンパイルしません。

したがって、現在の `cargo test --workspace` が成功したとしても、これらのファイルがビルド・テストされている保証はありません。

さらに `postgres/src/create_account.rs` は `bookkeeping_workflows::create_account` を参照しています。片方の宣言だけ追加すると、もう片方が未公開であることによるコンパイルエラーが起きます。

まず両側を適切に module 宣言へ追加し、コンパイルを確認してください。

## 3. 訂正仕訳の計上日時と会計期間が一致しない可能性

重要度：高・会計上の整合性リスク

[postgres/src/correct_purchase.rs](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/correct_purchase.rs#L193-L202) では、訂正仕訳の日時を `now` にしています。

```
journals::occurred_at.eq(now)
```

また、[L328–339](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/correct_purchase.rs#L328-L339) では Ledger Entry も `now` です。

しかし、訂正 Command は `effective_period` を別に持っており、その期間が OPEN であることを検証しています。

例えば、現在が10月でも9月がまだ OPEN であり、9月を訂正の計上月として指定できるモデルなら、次の不一致が起きます。

| 情報 | 値 |
| --------------------------------------------- | ---------- |
| `effective_period` | 2026-09 |
| `purchase_corrections.effective_period_start` | 2026-09-01 |
| `journals.occurred_at` | 2026-10-15 |
| Journal の日時で集計する会計月 | 2026-10 |

特に口座残高分析などが `journals.occurred_at` を参照する場合、計上月と集計月がずれます。

修正するなら、`recorded_at`（システムが記録した日時）と `effective_at` / `accounting_date`（会計上の計上日時）を分け、どの処理がどちらを使うかを定義するべきです。

## 4. 冪等性チェックより先に、現在の業務状態を検証している

重要度：高・再送時の挙動不一致

`persist_create_purchase` では、最初に現在の `default_allocation_version` を検証してから、既存の冪等性キーを検索しています。

[postgres/src/create_purchase.rs L102–139](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/create_purchase.rs#L102-L139)

次のケースが成立します。

1. 初回 `createPurchase` が成功し、冪等性レコードを作成する。
2. Household のデフォルト負担ルールを変更する。
3. ネットワーク再送などで、同じ冪等性キーと同じリクエストが再到着する。
4. 既存結果へ到達する前に `AllocationRuleMismatch` が返る。

さらに後続の処理では会計期間の Open 検証も実施しています。

一度成功したリクエストの再送なのに、現在の状態変化によって失敗してしまうのは、期待する冪等性と矛盾します。

修正案は、認証・認可とリクエスト識別を行った後、状態変更に依存する業務検証より先に、確定済みの冪等性結果を検索することです。

新規実行と既存結果の再取得を区別してください。

`persist_post_purchase` では既に先に冪等性キーを予約しているので、こちらの考え方へ統一するとよいです。

## 続いて、設計として早めに見直したい点

以下は、現時点で必ず誤動作しているというより、これから実装を追加すると構造的な問題になりそうな箇所です。

## 5. PostPurchase の Load → Decide → Persist が同一トランザクションで完結しない

重要度：高・Workflow の結合方法

これは今後の実装に最も影響する問題だと思います。

現状のコードは、

- [post_purchase_loader.rs L56](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/post_purchase_loader.rs#L56)：既存の `&mut AsyncPgConnection` を受けてロック付きで状態を読み込む。
- [post_purchase.rs L53](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/post_purchase.rs#L53)：`PgPool` から新しい接続を取得してトランザクションを開始し、Decision を保存する。

という別々の関数になっています。

このまま結合すると問題があります。

接続 A

BEGIN

Load + FOR UPDATE

Decide

接続 B

BEGIN

Persist → 更新待ち

COMMIT できない

A のロックを保持したまま B で保存するとロック待ちになり、A を先に commit すると Load から Persist までの原子性を失います。

まだ接続する Runner が実装されていないので、現時点の障害と断定はしません。しかし、この API 設計のままでは安全な実行順序を作りにくいです。

推奨する修正は、Application Runner がトランザクションを所有し、Load・Decide・Persist を同じ接続で実行することです。

```
async fn execute_post_purchase(
    pool: &PgPool,
    input: PostPurchaseInput,
) -> Result<PostPurchaseReceipt, Error> {
    let mut conn = pool.get().await?;

    conn.transaction(|tx| {
        Box::pin(async move {
            let facts = load_posting_facts(tx, &input).await?;

            let decision = decide_post_purchase(facts)?;

            persist_posting_decision(tx, decision).await
        })
    })
    .await
}
```

概念コードですが、重要なのは `persist_posting_decision` が `PgPool` ではなく同じトランザクション接続を受け取ることです。

他の Workflow でも、この原則を統一することを勧めます。

## 6. Workflow にドメイン型が集中し、Domain crate が計算ライブラリになりつつある

重要度：中・DMMF への忠実性

[bookkeeping-domain](https://github.com/walnuts1018/horse/tree/e8565c4/crates/bookkeeping-domain/src) は現在 `money.rs` と `allocation.rs` が中心です。

一方、[bookkeeping-workflows/src/lib.rs](https://github.com/walnuts1018/horse/blob/e8565c4/crates/bookkeeping-workflows/src/lib.rs) には `PurchaseId`、`AccountId`、`MemberId`、`PurchaseDraft`、`MemberFundingBasis`、`Payment`、`JournalLine` などの型が定義されています。

これは今すぐ動作不良を生むわけではありませんが、Domain が汎用計算、Workflow が業務モデル、という分離になりかねません。

DMMF の考え方では、業務上の意味を持つ型はまず Domain Model の一部として捉えるのが自然です。

私なら、

- `bookkeeping-domain` に Purchase、Journal、Funding、各種 ID、Money、Allocation などの型と純粋なドメイン関数を置く。
- `bookkeeping-workflows` に Workflow 固有の Command、処理段階、結果、Workflow Error、合成関数を置く。

という分離を基準にします。

また、現状の `PostPurchaseCommand` は、`accounting_period_open: bool` や `PurchaseDraft { status: PurchaseStatus }` のように、検証済みの事実と未検証の入力が一つの構造体に混在しています。

`PurchaseDraft` に `Posted` や `Other` を指定できる構造より、状態に応じて異なる型を持たせた方が、DMMF 第6〜7章に忠実です。

### 特に `MemberId` を `PoolId` に変換している箇所

[bookkeeping-workflows/src/lib.rs](https://github.com/walnuts1018/horse/blob/e8565c4/crates/bookkeeping-workflows/src/lib.rs) の `shares()` では、メンバー ID を文字列経由で `PoolId` に変換し、既存の配分計算を再利用しています。

```
let member_as_pool =
    PoolId::new(weight.member_id.as_str().to_owned())
        .expect("member IDs are nonempty");
```

これは DMMF の型による意味の区別を破ってしまっているので、今のうちに変えたいです。

配賦アルゴリズム自体が共通なのであれば、識別子をすり替えるのではなく、

```
pub struct WeightedShares<Id> {
    weights: Vec<(Id, NonZeroU64)>,
}
```

のように一般化するか、Pool 用と Member 用の意味的に異なる型を作り、計算部分だけを共通化するとよいです。

抽象化のために型安全性を下げないことが重要です。

## 7. ClosePeriod の精算結果を永続化時に再検証していない

重要度：高・今後の会計整合性リスク

[close_period.rs](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/close_period.rs) の `persist_close_period` は、会計期間の revision、下書き購入の有無、対象購入の revision を再検証しています。

この点は良い実装です。

しかし、精算金額自体が最新の入金・送金・調整情報に基づくことまでは再検証していません。

渡された `decision.settlements` をそのまま `settlement_run_lines` に書き込んでいます。

例えば、精算を計算した直後に別の送金が確定した場合、その計算結果が古くなっても、その送金を考慮せずに期間を閉じる余地があります。

同じトランザクション内で、必要な精算元データを読み取り、期間ロックを保持したまま `decide_close_period` を実行するようにしてください。

これは DMMF 第6章 §6.5 の、型での保証と集約・トランザクションの整合性を分ける話に相当します。

## 8. Event の revision が業務上の revision と結び付いていない

重要度：中

[postgres/src/correct_purchase.rs L341–358](https://github.com/walnuts1018/horse/blob/e8565c4/crates/postgres/src/correct_purchase.rs#L341-L358) では、訂正イベントを次の値で保存しています。

```
events::revision.eq(1_i64)
```

一方、[20261015160000_event_revision_contract.sql](https://github.com/walnuts1018/horse/blob/e8565c4/rust-migrations/20261015160000_event_revision_contract.sql) では Event の一意制約が削除されています。

同じ購入に対して複数の訂正が起きても、イベント上はすべて revision 1 になります。

もし revision が対象 Entity の改訂番号を意味するなら、これは誤りです。

修正は、`entity_revision` と `event_id`、必要なら Event Stream の `sequence` を別概念として定義することです。

複数の訂正を区別したいのであれば、定数 revision を使用して一意制約を外すのではなく、それぞれの訂正の ID やイベント ID で区別します。

## 9. アクティブメンバーの認可が Adapter 間で統一されていない

重要度：中・API 接続前に対応

`persist_create_purchase` では、Household Membership に対して `left_at.is_null()` を確認しています。

しかし、`post_purchase.rs` の `validate_household_references()` では、Actor が `household_members` に存在することは確認しているものの、脱退済みかどうかを確認していません。

同様に `persist_close_period` は、Actor の現在の権限を直接確認していません。

もちろん、今後の Application 層で認可するつもりなら、Adapter 自体が全権限検査を行う必要はありません。

ただし、DB 内のメンバー参照整合性と、現在の操作権限は異なります。

どの操作を誰が行えるかを Household BC の契約として定義し、すべての Workflow Runner が一貫して検証できるようにするとよいです。

## 今の実装で、むしろ維持したい部分

問題ばかりではなく、既に良い方向へ進んでいる箇所もあります。

`Yen` と `PositiveYen` を Smart Constructor と非公開フィールドで定義している点、最大剰余法で金額保存と決定的な端数処理を実装している点、`decide_post_purchase` が DB に依存しない点、Journal の貸借一致を確認している点は維持したいです。

また、PostgreSQL Adapter に Household ID のスコープ確認、行ロック、revision チェック、冪等性レコードなどが入り始めているのも適切です。

ここで問題なのは Diesel や Atlas の選択ではなく、純粋な業務判断を、実際の整合性が保証された DB 状態にどう結び付けるかという設計です。

## 修正する順序

| 優先 | 対応 | 理由 |
| -- | -------------------------------- | ---------------- |
| 1 | 締め後訂正の状態モデルを修正 | 明確な業務要件との矛盾 |
| 2 | 未登録 module をコンパイル対象にする | 以降の実装の検証漏れを防ぐ |
| 3 | Workflow Runner とトランザクションの所有権を決定 | 後から変更すると影響が大きい |
| 4 | ClosePeriod の精算計算・ロックを統合 | 確定金額の整合性 |
| 5 | 冪等性の再送経路・訂正日時を修正 | 履歴・再送の正しさ |
| 6 | Domain 型の配置と ID の混用を修正 | DMMF のモデルとしての一貫性 |
| 7 | Event revision と認可契約を統一 | API 接続前に必要 |

### 総評

現状は、全面的に設計をやり直す段階ではありません。

むしろ、純粋関数、型付き金額、明示的な Decision、トランザクション付き Adapter という基本的な方向は合っています。

ただ、今後のコードが増える前に、次の2点だけは確定させたいです。

1. Domain の型が業務上の不変条件と状態を表現し、Workflow がその状態遷移を組み立てる。
2. Application Runner が I/O とトランザクションを所有し、純粋な Decision を整合した状態から実行する。

この2点が固まれば、Diesel による永続化実装が増えても、DMMF の設計原則から大きく外れずに開発できるはずです。

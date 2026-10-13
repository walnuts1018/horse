# Horse実装レビュー（2026年10月13日）

## 1. 商品候補検索の性能計測

[`internal/postgres/query/product_suggestions.go`](../../internal/postgres/query/product_suggestions.go)は完全一致・前方一致の候補を部分一致候補より先に取得し、部分一致候補を最大500件に制限します。関連する商品と別名をまとめて読み込み、候補の順位付けはGo側で行います。部分一致検索は先頭ワイルドカードを使うため、通常のB-tree索引を利用できません。

部分一致検索の実データ量に対する応答時間とDB読取量は計測していません。実測で遅延が確認された場合に、PostgreSQLのtrigram索引とDB側ランキングを検討します。

## 2. 【高】購入訂正の精算計算が、購入時点ではなく現在の負担ルールを参照する

該当: [`mutations_purchase_correction.go`](https://github.com/walnuts1018/horse/blob/6079b358539bc9819ec1d3dcfa0ce13e6265b448/internal/graph/mutations_purchase_correction.go#L379-L424)

購入確定時の負担スナップショットや、訂正のメンバー別負担スナップショットは保存されています。ここは適切です。

しかし、訂正時に `settlement_adjustments` を生成する経路では、次のように現在有効なプールの資金拠出ルールを読み直しています。

```
rules, err := poolFundingRules(
    ctx, bobpgx.NewTx(tx, nil), input.HouseholdID,
)
changes, err := domain.CalculateExpenseAdjustments(
    fundingExpenses, rules,
)
```

例えば、購入時点ではA・Bが50:50で負担していたものを、後から100:0へ変更し、その購入を返金訂正すると、訂正の精算額に新しい負担設定が混入する可能性があります。

一方、[`snapshotCorrectionMemberBurden`](https://github.com/walnuts1018/horse/blob/6079b358539bc9819ec1d3dcfa0ce13e6265b448/internal/graph/member_burden.go#L155-L226) は保存済みの購入時点の負担情報を参照しています。つまり、訂正の負担履歴と精算調整額で参照するルールが食い違う構造です。

修正方針: 訂正時の決済元・負担先に関する計算も、元の購入のスナップショットを使用するよう統一してください。購入確定後に拠出比率・所有者・精算ポリシーを変更してから返品する統合テストが必要です。

## 5. 【中】原本の照合APIが、家庭の抽出履歴を毎回全件取得する

該当: [`internal/api/reconciliation.go`](https://github.com/walnuts1018/horse/blob/6079b358539bc9819ec1d3dcfa0ce13e6265b448/internal/api/reconciliation.go#L78-L171)

`documentMatches` は、同じ家庭の `status=succeeded` の抽出ジョブを `.All()` で全件読み込み、その後アプリケーション側で文書IDを集約しています。

最終的な照合対象は直近100文書なのに、その100件を決める前に、過去の成功ジョブをすべて取得しています。

取り込み履歴が増えると、1件の照合操作でも大量のDB行を転送・走査することになります。

修正: PostgreSQL側で `EXISTS`、`DISTINCT ON`、または適切なサブクエリを使い、成功ジョブの存在を条件として直近100文書を直接取得してください。必要なジョブ結果だけを後段で取得する構成がよいです。

これは正確性より性能の問題ですが、CSVやメールを長期的に取り込むシステムなので、今のうちに直す価値があります。

## 6. 【中】負担ルール一覧にN+1クエリが残っている

該当: [`internal/graph/query_allocation_rules.go`](https://github.com/walnuts1018/horse/blob/6079b358539bc9819ec1d3dcfa0ce13e6265b448/internal/graph/query_allocation_rules.go#L64-L104)

一覧ページの行を取得した後、各行について `loadAllocationRule` を呼んでいます。この関数は再度ルール本体を読み、対象カテゴリ・商品・プール・配賦設定も取得するため、ページ件数に比例してクエリが増えます。

既に購入一覧や `nodes` ではバッチ読み込みを導入できているので、同じ方法を適用できます。

修正: 一覧で取得したルール行を再利用し、対象エンティティと `AllocationRuleWeights` をまとめて読み込んでください。

## 現時点で評価できる実装

特に次の部分は、コードや既存テストから設計意図を確認できました。

- 家計単位の認可: GraphQLや原本取得で家庭への所属確認があり、招待の発行はOWNERに制限されています。
- 購入と会計の原子性: Application Serviceへの移行が進み、購入・仕訳・証拠・冪等性・イベントを同じトランザクションで扱っています。
- 過去の負担情報: 元購入のメンバー別負担と決済元のスナップショットを保存する構造があります。ただし、上記2の訂正処理はDB統合テストのCI確認が残っています。
- ジョブの耐障害性: PostgreSQLをジョブ状態の正本とし、RabbitMQを通知に使う構成には合理性があります。現段階でWatermillなどへ置き換える必要はありません。
- GraphQL: SDL分割、Mutation Result、Relay NodeとConnectionへの対応が進んでいます。
- DB制約: ルートカテゴリ名の重複を防ぐ `UNIQUE NULLS NOT DISTINCT` も、後続マイグレーションで既に導入されています。これは修正不要です。

## 実装の不足と、コードの不具合は分けて考える

[`docs/completion-audit.md`](https://github.com/walnuts1018/horse/blob/6079b358539bc9819ec1d3dcfa0ce13e6265b448/docs/completion-audit.md) には、現時点の未検証項目がかなり具体的に記録されています。

とりわけ、実際の日本語レシートでの税率混在、ポイント・クーポン・返金の照合、Geminiと独立OCRの比較、異なるユーザーによる家庭への参加、オフライン競合の実操作は完成検証が残っています。これらは実装が存在しないという断定ではなく、正常に機能するという証拠がまだ不足している状態です。

GitHub Actions run `38409639022`は`Lint Go`で失敗し、commit `959a3af`のrun `38412209424`は原本照合のDB統合テストで失敗しました。`EXISTS`副問い合わせの定数をtext型で渡す修正を行い、最新migrationを適用した一時DBで対象テストが成功しました。一時DBは削除済みです。購入訂正と負担ルール一覧のDB統合テストは新しいGitHub Actionsでの成功を確認するまで残します。

## 残っているレビュー項目

| 優先 | 対象 | 確認事項 |
| -- | ------------------ | ------------------ |
| P1 | 2. 訂正での過去ルールの利用 | DB統合テストのCI通過 |
| P2 | 5. 原本照合の全件走査 | DB統合テストのCI通過 |
| P2 | 6. 負担ルール一覧のN+1 | DB統合テストを含む変更後の挙動確認 |
| P3 | 商品候補検索の性能計測 | 実データ量での応答時間とDB読取量の計測 |

総評: アーキテクチャ全体を作り直す必要はありません。特に会計ロジックを整数円で扱い、履歴スナップショットと冪等性を重視している方針は維持すべきです。

ただし、今の段階で最も警戒すべきなのはUIやコードの分割ではなく、返品・訂正・精算が組み合わさったときの会計不変条件です。ここを先に固め、その後にN+1や既存の設計レビュー項目を解消する順番を推奨します。

今回のレビューはリポジトリのソースコード、マイグレーション、既存テスト、CI情報に基づく静的レビューです。新たなDB統合テストの実行や実環境での操作は行っていません。

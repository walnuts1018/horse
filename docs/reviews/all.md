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

## 4. 【中】購入全体の訂正で、同じ負担比率を指定しても拒否される

該当: [`mutations_purchase_correction.go`](https://github.com/walnuts1018/horse/blob/6079b358539bc9819ec1d3dcfa0ce13e6265b448/internal/graph/mutations_purchase_correction.go#L23-L91)

`correctionAllocationWeights` は、品目単位なら元の `weight`、購入全体ならプール別の `amount_yen` 合計を返します。

その一方で `sameAllocationWeights` は、入力された `weight` と保存値の数値そのものを比較しています。

例えば、購入全体の負担額が A=100円、B=100円なら、A:B=1:1を指定しても、保存値の100:100と一致しないため拒否されます。

現在のWeb UIは品目単位の返品を呼び出しているので、直ちにその画面が壊れる問題ではありません。しかし、公開済みのGraphQL APIは購入全体の訂正も受け付けるため、API契約に不整合があります。

修正: 比率の同値性を比較するか、そもそも訂正に `allocations` を再指定させない設計へ統一してください。保存済みの配賦が正本なら、後者のほうが自然です。

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

## 7. 【中〜低】Geminiのモデルを変更すると費用がゼロ円扱いになる場合がある

該当: [`internal/ingestion/router.go`](https://github.com/walnuts1018/horse/blob/6079b358539bc9819ec1d3dcfa0ce13e6265b448/internal/ingestion/router.go#L248-L278)

モデル名は環境変数で変更できますが、`modelPrices` が対応するのはハードコードされた一部のモデルだけです。未知のモデル名では `(0, 0)` を返すため、実際には有料で実行されていても推定費用が0円になります。

Google側のSpend Capとは別に、Horseの費用記録を信頼できなくなる問題です。

修正: 未登録モデルは `unknown_price` として扱い、ゼロ円と区別してください。有料ルートでは、価格情報が未登録の場合に実行を拒否するか、明示的に管理者が許可する方式が安全です。また、為替レートの初期値 `158.535` を固定値のまま長期間使わないようにすると、費用表示の精度も改善します。

## 現時点で評価できる実装

特に次の部分は、コードや既存テストから設計意図を確認できました。

- 家計単位の認可: GraphQLや原本取得で家庭への所属確認があり、招待の発行はOWNERに制限されています。
- 購入と会計の原子性: Application Serviceへの移行が進み、購入・仕訳・証拠・冪等性・イベントを同じトランザクションで扱っています。
- 過去の負担情報: 元購入のメンバー別負担と決済元のスナップショットを保存する構造があります。ただし、上記2の訂正処理には修正が必要です。
- ジョブの耐障害性: PostgreSQLをジョブ状態の正本とし、RabbitMQを通知に使う構成には合理性があります。現段階でWatermillなどへ置き換える必要はありません。
- GraphQL: SDL分割、Mutation Result、Relay NodeとConnectionへの対応が進んでいます。
- DB制約: ルートカテゴリ名の重複を防ぐ `UNIQUE NULLS NOT DISTINCT` も、後続マイグレーションで既に導入されています。これは修正不要です。

## 実装の不足と、コードの不具合は分けて考える

[`docs/completion-audit.md`](https://github.com/walnuts1018/horse/blob/6079b358539bc9819ec1d3dcfa0ce13e6265b448/docs/completion-audit.md) には、現時点の未検証項目がかなり具体的に記録されています。

とりわけ、実際の日本語レシートでの税率混在、ポイント・クーポン・返金の照合、Geminiと独立OCRの比較、異なるユーザーによる家庭への参加、オフライン競合の実操作は完成検証が残っています。これらは実装が存在しないという断定ではなく、正常に機能するという証拠がまだ不足している状態です。

また、今回の対象コミットのGitHub Actionsでは、確認できた `quality` と `publish` のチェックがいずれも `cancelled` でした。以前のコミットに成功したCIがあることと、今回レビューしたHEADがCIを通過したことは区別する必要があります。

## 修正優先順位

| 優先 | 対象 | 理由 |
| -- | ------------------ | ------------------ |
| P0 | 3. 返金額の全体上限 | 会計上あり得ない状態を保存できる |
| P1 | 2. 訂正での過去ルールの利用 | 負担額・精算額が誤る可能性 |
| P1 | 1. CSV明細の一意性 | 異なる明細を誤って同じ購入に紐付ける |
| P2 | 4. 訂正配賦の比率比較 | 正当なGraphQL入力を拒否 |
| P2 | 5. 原本照合の全件走査 | 取り込み履歴が増えるほど遅くなる |
| P2 | 6. 負担ルール一覧のN+1 | 一覧のクエリ数が増大 |
| P3 | 7. Gemini費用の未登録モデル | 料金表示・監査記録が不正確 |

総評: アーキテクチャ全体を作り直す必要はありません。特に会計ロジックを整数円で扱い、履歴スナップショットと冪等性を重視している方針は維持すべきです。

ただし、今の段階で最も警戒すべきなのはUIやコードの分割ではなく、返品・訂正・精算が組み合わさったときの会計不変条件です。ここを先に固め、その後にN+1や既存の設計レビュー項目を解消する順番を推奨します。

今回のレビューはリポジトリのソースコード、マイグレーション、既存テスト、CI情報に基づく静的レビューです。新たなDB統合テストの実行や実環境での操作は行っていません。

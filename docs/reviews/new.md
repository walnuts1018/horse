# Horse コードベース追加レビュー

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse) の `main`、コミット `f0d6ac3a`（2026年10月15日確認）。

既存の `docs/reviews/design.md`、`gemini.md`、`lack.md`、`simple.md`、`uiux.md` を確認し、すでに書かれている改善事項は除外しました。

今回は機能の有無やUI/UXだけでなく、Goのロジック、PostgreSQL、GraphQL、React、Relay、IndexedDB、AI処理、CIまで対象を広げています。

特に優先して修正すべき新規問題は以下です。

| 重要度 | 新規指摘 | 影響 |
| --- | ------------------------------------ | --------------------- |
| P1 | 口座残高分析でページング前に全取引を解決 | 取引が増えるほど応答が遅くなる |

以下、コード上の根拠と修正方法を具体的に説明します。

## 1. P1：口座残高分析はページング前に全取引を解決している

該当コード

- [account_balance_analysis.go L41–103](https://github.com/walnuts1018/horse/blob/main/internal/graph/account_balance_analysis.go#L41-L103)
- [query/account_balance_analysis.go L42–152](https://github.com/walnuts1018/horse/blob/main/internal/postgres/query/account_balance_analysis.go#L42-L152)

`accountBalanceAnalysis()` では、先に全ての残高変動を取得します。

その後、

```
for _, change := range analysis.Changes {
    source, err := r.accountBalanceSource(
        ctx, householdID, analysis.Account, change.Journal,
    )
    // ...
}
```

で各変動の参照先をGraphQLオブジェクトに変換します。

しかし、`first` や `after` によるページング処理は、その後に行われています。

例えば、10,000件の履歴がある口座で `first: 20` を指定しても、10,000件分の `accountBalanceSource()` が実行されます。

しかも `accountBalanceSource()` は `r.node()` などを通じて追加のDB読み出しを実行します。

これは、単なる巨大関数の問題ではなく、ページングが負荷削減として機能していない問題です。

さらに `AccountBalanceAnalysisForPeriod()` 自体も、対象口座の全仕訳を `.All()` でメモリに読み込んでいます。

改善方法

残高はSQLのWindow Functionを使って計算するのが適しています。

例えば、

```
SUM(delta_yen) OVER (
    PARTITION BY account_id
    ORDER BY occurred_at, journal_id
)
```

のように累積残高を求められます。

期首残高の算出と表示ページの取得は分離し、DBから表示対象の行を絞り込んだあと、対応するSource Nodeを一括解決します。

全履歴をメモリに読み込んでからページングする必要はありません。

## 1. P1：購入Subscriptionのたびに一覧を再取得している

該当コード：[use-household-purchases.ts](https://github.com/walnuts1018/horse/blob/main/web/src/entities/purchase/api/use-household-purchases.ts#L193-L226)

購入変更を受信すると、以下の `refresh()` が呼ばれます。

```
const refresh = useCallback(() => {
  setMoreErrorState(null);
  setCorrectionPages({});
  refetch(variables, { fetchPolicy: "network-only" });
}, [refetch, variables]);
```

問題は2つあります。

第一に、品名1文字の更新でも購入一覧を再取得することです。

第二に、`setCorrectionPages({})` によって別途読み込んだ訂正履歴の状態もクリアしてしまいます。

同じ購入を複数人が頻繁に編集する場合、通知が増えるほど不要なデータ取得が発生します。

改善方法

Relayのストア更新とSubscriptionのイベント情報をもっと活用します。

現在の変更イベントにはEntity ID、Revision、変更フィールドが含まれています。変更対象の購入だけ再取得するか、受信した更新内容をストアに反映する方式にします。

一覧全体の再取得が必要なときも、短時間の複数イベントをまとめることを検討します。

また、訂正履歴のキャッシュをクリアするのは、その購入の訂正履歴が実際に変更された場合に限定するべきです。

## 1. P1：大量のカード・銀行明細の照合が全件走査になっている

該当コード

- [api/reconciliation.go](https://github.com/walnuts1018/horse/blob/main/internal/api/reconciliation.go)
- [ingestion/reconcile.go](https://github.com/walnuts1018/horse/blob/main/internal/ingestion/reconcile.go)
- [document-matches.ts](https://github.com/walnuts1018/horse/blob/main/web/src/pages/imports/api/document-matches.ts)

`documentMatches()` は、世帯の既存Observationをすべて読み込んだあと、各入力Observationと順番に照合しています。

```
existing, err := reconciliationObservations(
    r.Context(),
    executor,
    document.HouseholdID,
    nil,
)
```

`reconciliationObservations()` ではSQL上の件数制限や候補の絞り込みを行わず、`.All()` を使っています。

処理量は大まかに、

\\[ O(\text{今回の明細数}\times\text{過去の全Observation数}) \\]

になります。

さらに、今回のObservationは100件で切り詰められていますが、取得クエリに明示的な並び順がありません。したがって100件を超えるCSVで、どの行の照合候補が返るか安定しません。

フロントの `readDocumentMatches()` も、APIが提供する `sourceReference` による特定行検索を利用していません。

改善方法

最初にDBで候補を絞り込みます。

取引番号・注文番号・金額・日時・店舗・カード末尾など、照合に使うフィールドを検索条件とし、候補が少数になってからGoの詳細照合処理を実行します。

同一注文の分割発送やカード明細の表記ゆれも考慮して、条件を厳密にしすぎないことは重要です。

APIは原本の全行を一括処理する方式ではなく、安定したカーソルと `sourceReference` による行単位検索にも対応させます。

これにより、大量のカード明細を取り込んでも照合処理を実用的な負荷に抑えられます。

## 11. P2：一部の月別グラフが負の金額を正しく表現できない

該当コード：[analysis-breakdown.tsx](https://github.com/walnuts1018/horse/blob/main/web/src/pages/analysis/ui/analysis-breakdown.tsx#L81-L91)

カテゴリ別グラフでは、正負の金額を分離した表示が実装されています。

ところが、それ以外のディメンションでは次の計算になっています。

```
style={{
  height: `${Math.max(3, (row.amount / max) * 100)}%`,
}}
```

例えば月間支出が−5,000円だった場合でも、棒の高さは最低3%になります。

金額ラベルは負なのに、グラフの棒は正の支出と同じ方向に表示されます。

返金や訂正を含む家計簿では、負の金額は正常に発生するため、カテゴリ以外のグラフにも正負を分離したスケールを採用するべきです。

## 12. P2〜P3：コード品質・パフォーマンス・テストの追加指摘

ここからは小さな改善も含めて整理します。

| 優先度 | 対象 | 新規指摘と推奨修正 |
| --- | ------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| P2 | [CI](https://github.com/walnuts1018/horse/blob/main/.github/workflows/ci.yaml) | OCRのLintは実行していますが、`services/ocr/test_app.py`の単体テストを実行するステップがありません。PythonのテストもCIに組み込む |
| P2 | [照合判定](https://github.com/walnuts1018/horse/blob/main/internal/ingestion/reconcile.go#L42-L87) | `TransactionID`の一致を強く評価していますが、取引IDの発行元が一致しているかは検証していません。異なる決済事業者が同じIDを使用した場合に備え、発行元・識別子の名前空間を管理する。現状は自動統合ではなく候補提示なので、影響は誤った一致評価が中心 |
| P2 | [Subscription](https://github.com/walnuts1018/horse/blob/main/internal/graph/purchase_subscription.resolvers.go) | 接続ごとに1秒間隔で所属確認とイベント検索を行っています。同時接続数に比例してDBクエリが増えるため、通知ベースの起床と定期的な権限再検証を組み合わせる |
| P2 | [分析画面の日付](https://github.com/walnuts1018/horse/blob/main/web/src/pages/analysis/ui/analysis-page.tsx#L180-L190) | `today`を`useState`の初期値として一度しか計算していません。画面を開いたまま日付が変わると、定期支出の有効・無効判定が更新されない |
| P3 | [sorted()](https://github.com/walnuts1018/horse/blob/main/web/src/pages/analysis/ui/analysis-page.tsx#L19-L28) | ソートを挿入ソートとして自前実装しています。`toSorted(compare)`に置き換えればコードが短くなり、一般的な実装に任せられる |
| P3 | [sortBy()](https://github.com/walnuts1018/horse/blob/main/web/src/pages/analysis/ui/item-price-panel.tsx#L18-L26) | 上記と同じ挿入ソートが別名で再実装されています。両方ともネイティブのソートへ置き換え可能 |
| P3 | [generatedUUID()](https://github.com/walnuts1018/horse/blob/main/internal/graph/mutation_support.go#L29-L32) | UUID変換エラーを無視してNil UUIDを返すヘルパーがあります。エラーを返すか、妥当性が保証された入力に用途を限定した型・命名に変更する |
| P3 | [lint:changed](https://github.com/walnuts1018/horse/blob/main/mise.toml) | CIのGo Lintは`--new-from-rev`を使用しています。差分Lint自体は合理的ですが、全体Lintも定期的に実行することで、既存コードや生成物に残った問題を検出できる |

いずれも既存レビューの「大きな関数を分割する」「レイヤーの責務を整理する」といった一般的な指摘とは別の、具体的な実装改善です。

なお、GitHub Actionsの `parallel` は2026年6月に正式導入された構文なので、これ自体を問題として指摘する必要はありません。

[image](https://www.google.com/s2/favicons?domain=https://github.blog\&sz=32)

GitHub Changelog

## 13. 追加してほしいテスト

新規指摘に対応するテストも、既存のテストファイルとは別の観点から追加することをおすすめします。

| テスト対象 | 検証するケース |
| -------------------- | ------------------------------------ |
| 口座分析 | 10,000件の履歴でも20件だけ表示対象を解決する |
| Subscription + Relay | 複数更新の受信で訂正履歴が不必要に消去されない |
| Outbox | 購入確定成功後に一覧の再取得だけ失敗する |
| 明細照合 | 100行を超えるCSVでも特定行の候補を取得できる |
| 日付 | 分析画面を開いたまま日付・月が変わる |
| 月別グラフ | 月間支出が負になった場合の表示 |

また、UIのテストでは、購入更新時のHTTPリクエスト件数や、ページング時に実行したGraphQL Query数も計測するとよいです。

## 14. 修正優先順位

今回の指摘を修正するなら、以下の順序を推奨します。

01

大量データへの対応

口座分析のDBページング、照合候補のSQL絞り込み、Relay Subscriptionの再取得を最適化します。

02

OCR・Outbox・小規模コード品質改善

Outboxの失敗境界、CIテスト、不要な独自ソートや線形探索を修正します。

## 総評

今回新しく見つかった問題には、既存レビューが重点を置いていた「コードの責務分離」とは異なる傾向があります。

特に重要なのは、個々の関数が正しく動いていても、コンポーネントをまたいだときに整合性や性能が崩れることです。

今回のレビューでは、既存レビューにない複数の主な指摘を抽出しました。

一方、コードベースは1,270ファイル規模であり、今回の調査はGitHub上のソースコードを中心とした静的レビューです。全ファイルの逐行検査や、実際のKubernetes環境での負荷・動作テストまで完了したものではありません。

そのため、重要度はコードから確認できる影響に基づく評価であり、障害が本番で発生したことを確認したものではありません。今回の指摘については、再現テストを追加したうえで修正すると、既存レビューでは見落としていた品質の改善につながります。

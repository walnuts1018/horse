# Horse の保守性・シンプルさのレビュー

対象はHorseの`main`です。以前の指摘を現行コードと照合し、実装済みの内容を除いて残る課題を記録します。

## 1. 高優先：画面コンポーネントが大きい

主な画面ファイルは今も大きく、複数の業務機能と状態を一つのコンポーネントが所有しています。

| ファイル | 行数 | `useState`の呼び出し数 |
| --- | ---: | ---: |
| [settings-page.tsx](../../web/src/pages/settings/ui/settings-page.tsx) | 149 | 0 |
| [ledger-page.tsx](../../web/src/pages/ledger/ui/ledger-page.tsx) | 1,148 | 3 |
| [imports-page.tsx](../../web/src/pages/imports/ui/imports-page.tsx) | 756 | 7 |
| [purchase-create-page.tsx](../../web/src/pages/purchase-create/ui/purchase-create-page.tsx) | 498 | 9 |
| [settlement-page.tsx](../../web/src/pages/settlement/ui/settlement-page.tsx) | 287 | 8 |
| [analysis-page.tsx](../../web/src/pages/analysis/ui/analysis-page.tsx) | 647 | 10 |

親画面には画面遷移、検索条件、読込結果の調整が残っています。特に`LedgerPage`、`ImportsPage`、`SettlementPage`は一覧と複数の操作状態を持つため、次の分割では機能単位で変更できる範囲を基準にします。

## 2. 中優先：画面固有CSSと重要な処理の境界

[global.css](../../web/src/app/styles/global.css)は4,194行あり、画面固有規則が残っています。既存の画面別CSS moduleへ移せる規則を引き続き確認します。`web/src`にはフロントエンドのテストランナーがなく、購入データ変換やOutboxなどの重要な純粋処理を安定して検証する手段がありません。

## 3. 低優先：GraphQLの購入読み込みが広い関連データを組み立てる

[loadPurchases](../../internal/graph/helpers.go)は購入に対し、口座、証拠、確認、品目、支払、訂正、品目配賦、タグなどをまとめて読み込み、GraphQLモデルを構築します。Bobのバッチローダーを利用してN+1は避けていますが、一覧で一部のフィールドしか使わない場合にも広い関連データを読み込みます。

今すぐDataLoaderを追加する必要はありません。購入一覧と購入詳細で必要な読み取りデータが異なる箇所を確認し、負荷または変更頻度の問題が出る場合に一覧・詳細の読み取りモデルを分ける選択肢があります。読み取り量や応答時間は計測されていないため、現時点では性能問題としては扱いません。

## 改善の優先順位

| 優先 | 対象 |
| --- | --- |
| P1 | 大きなReact画面の機能・状態分割 |
| P2 | 画面固有CSSと重要な処理の境界 |
| P3 | 必要性を測定した後の購入読み取りモデル分割 |

Bob DSLによる型付きDBアクセス、gqlgenとRelayのコード生成、会計不変条件を守るトランザクション、リビジョンによる競合検知、Outboxの冪等性保証は維持します。コード行数ではなく、一つの機能を変更するときに読む必要がある範囲を減らすことを改善基準にします。

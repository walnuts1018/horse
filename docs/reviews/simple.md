# Horse の保守性・シンプルさのレビュー

対象はHorseの`main`です。以前の指摘を現行コードと照合し、実装済みの内容を除いて残る課題を記録します。

## 画面固有CSSと重要な処理の境界

[global.css](../../web/src/app/styles/global.css)には共通規則と複数画面で使う購入行・見出し・パネル、および未分離の他画面固有規則が残ります。ホーム画面だけで使う規則は[home-page.css](../../web/src/pages/home/ui/home-page.css)へ、明細取込画面だけで使う明細レビュー規則は[imports-page.css](../../web/src/pages/imports/ui/imports-page.css)へ、精算画面の資金調達・借入規則は[settlement-page.css](../../web/src/pages/settlement/ui/settlement-page.css)へ、分析画面のコントロール規則は[analysis-page.css](../../web/src/pages/analysis/ui/analysis-page.css)へ移しました。残るレビュー範囲は、共通規則の所有先と他画面固有規則のpage sliceへの分離可否です。

## GraphQLの購入読み込み範囲

[loadPurchases](../../internal/graph/helpers.go)は購入に対し、口座、証拠、確認、品目、支払、訂正、品目配賦、タグなどをまとめて読み込み、GraphQLモデルを構築します。Bobのバッチローダーを利用してN+1は避けていますが、一覧で一部のフィールドしか使わない場合にも広い関連データを読み込みます。

今すぐDataLoaderを追加する必要はありません。購入一覧と購入詳細で必要な読み取りデータが異なる箇所を確認し、負荷または変更頻度の問題が出る場合に一覧・詳細の読み取りモデルを分ける選択肢があります。読み取り量や応答時間は計測されていないため、現時点では性能問題としては扱いません。

## 改善の優先順位

| 優先 | 対象 |
| --- | --- |
| P2 | 画面固有CSSと重要な処理の境界 |
| P3 | 必要性を測定した後の購入読み取りモデル分割 |

Bob DSLによる型付きDBアクセス、gqlgenとRelayのコード生成、会計不変条件を守るトランザクション、リビジョンによる競合検知、Outboxの冪等性保証は維持します。コード行数ではなく、一つの機能を変更するときに読む必要がある範囲を減らすことを改善基準にします。

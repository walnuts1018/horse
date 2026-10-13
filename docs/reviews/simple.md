# Horse の保守性・シンプルさのレビュー

対象はHorseの`main`です。以前の指摘を現行コードと照合し、実装済みの内容を除いて残る課題を記録します。

## 1. 高優先：画面コンポーネントが大きい

主な画面ファイルは今も大きく、複数の業務機能と状態を一つのコンポーネントが所有しています。

| ファイル | 行数 | `useState`の呼び出し数 |
| --- | ---: | ---: |
| [settings-page.tsx](../../web/src/pages/settings/ui/settings-page.tsx) | 2,190 | 55 |
| [ledger-page.tsx](../../web/src/pages/ledger/ui/ledger-page.tsx) | 2,431 | 18 |
| [imports-page.tsx](../../web/src/pages/imports/ui/imports-page.tsx) | 2,479 | 20 |
| [purchase-create-page.tsx](../../web/src/pages/purchase-create/ui/purchase-create-page.tsx) | 669 | 11 |

`SettingsPage`は口座、負担先、配賦、商品、タグ、予算、定期支出、資金移動、残高推移を扱います。招待管理と口座残高推移は同じ`pages/settings`sliceの専用UIへ分け、招待APIと残高照会operationも同sliceのAPI segmentへ移しました。`LedgerPage`は検索、一覧、選択、一括更新、購入編集、訂正、証拠、観測照合を扱います。`ImportsPage`は取込一覧、確認、送信、カメラ画像処理を扱い、画像座標変換は同じpage sliceのmodelへ分離しました。`PurchaseCreatePage`は購入共通項目と保存・送信を持ち、品目行編集、Undo、キーボード操作、貼付け、候補選択を同じ`pages/purchase-create`sliceの専用UIとmodelへ移しました。

画面の機能ごとにUIと状態の所有者を分け、親コンポーネントを配置と画面遷移の責務に限定する余地があります。単に処理を別ファイルや巨大なHookへ移すのではなく、機能が独立して変更できる境界を作る必要があります。

## 2. 高優先：Relay境界に手書き型と汎用変換が残っている

[household.ts](../../web/src/shared/api/household.ts)には、Relay生成型とは別の手書き型があり、`executeGraphql()`と`useGraphqlMutation()`の両方で`normalizeGraphQLValue()`を通します。

[use-household-purchases.ts](../../web/src/entities/purchase/api/use-household-purchases.ts)は生成されたPurchaseList fragmentの品目型を受け取る明示的なEntity mapperを使い、`as unknown as PurchaseItem[]`を解消しています。

購入作成・記帳と品目更新の変数変換はPurchase Entity内の生成型mapperへ移し、Mutation名による共通基盤の分岐と`as never`を削除しました。`normalizeGraphQLValue()`と手書き通信型は残っており、Relay生成型を通信境界で維持する必要があります。

## 3. 中優先：会計の共通規則が複数パッケージに重複している

会計期間計算、金額加算、イベント記録、冪等性検証は購入、送金、資金拠出、口座移動など複数のApplicationパッケージに同様の関数があります。各処理の将来の変更で規則が分岐する可能性があります。

口座の初期残高、口座間移動、外部入金は`accounting.BuildEntries()`と`InsertEntries()`を使うように統一し、借貸行の生成と検証の重複を解消しました。

共通化対象を会計期間、金額計算、イベント記録、冪等性、仕訳生成に限定し、既存の仕訳生成処理を各記帳経路から利用できるようにする余地があります。

## 4. 中優先：帳簿の検索条件がサーバー処理と画面処理に分かれている

購入検索では期間、検索語、分類、タグ、決済口座、負担先、起点、証拠有無をサーバー側で適用してからページングしています。全ページを無条件に取得してブラウザで絞るという以前の指摘は解消しています。

一方、[ledger-page.tsx](../../web/src/pages/ledger/ui/ledger-page.tsx)は購入表示と品目表示の表示条件を別々に実装し、サーバー検索後にも期間・検索語・分類・タグ・決済口座などを`filter()`と`flatMap()`で絞り込みます。金額の下限・上限は購入Queryの`minimumAmount`・`maximumAmount`でページング前に適用し、購入合計と品目額の対象も選べます。品目対象では金額・カテゴリ・タグ・負担先を同じ品目で判定し、返却する品目にも同じ条件を適用します。購入単位の範囲、品目単位の範囲、ページ境界、品目属性との組み合わせを検証する統合テストは追加しましたが、ローカルDBがないため実行結果はCI待ちです。

検索条件を単一のQuery入力へまとめ、金額条件もページング前に適用したうえで、購入表示と品目表示で共通化できる判定を共有する必要があります。

## 5. 中優先：画面固有CSSと重要な純粋処理が分離されていない

[global.css](../../web/src/app/styles/global.css)は4,871行あり、画面固有のスタイルも含みます。共通デザイントークンと汎用UIを残し、画面に固有の規則を機能単位のCSSへ分ける余地があります。

画像座標の行列演算は[importsのmodel](../../web/src/pages/imports/model/evidence-geometry.ts)へ分離しました。`web/src`には`.test.ts`、`.test.tsx`、`.spec.ts`、`.spec.tsx`がなく、`web/package.json`にもテスト用スクリプトがありません。購入データ変換、Outbox、矩形範囲の処理など、純粋関数として分離できる重要処理の検証は未整備です。

## 6. 低優先：GraphQLの購入読み込みが広い関連データを組み立てる

[loadPurchases](../../internal/graph/helpers.go)は購入に対し、口座、証拠、確認、品目、支払、訂正、品目配賦、タグなどをまとめて読み込み、GraphQLモデルを構築します。Bobのバッチローダーを利用してN+1は避けていますが、一覧で一部のフィールドしか使わない場合にも広い関連データを読み込みます。

今すぐDataLoaderを追加する必要はありません。購入一覧と購入詳細で必要な読み取りデータが異なる箇所を確認し、負荷または変更頻度の問題が出る場合に一覧・詳細の読み取りモデルを分ける選択肢があります。読み取り量や応答時間は計測されていないため、現時点では性能問題としては扱いません。

## 改善の優先順位

| 優先 | 対象 |
| --- | --- |
| P1 | 大きなReact画面の機能・状態分割 |
| P1 | Relay生成型と明示的mapperを中心にした通信境界 |
| P2 | 会計期間、金額計算、イベント、冪等性、仕訳生成の共通化 |
| P2 | 帳簿フィルターのQuery入力と画面表示の統合 |
| P2 | 画面固有CSSの分離と重要な純粋処理の検証 |
| P3 | 必要性を測定した後の購入読み取りモデル分割 |

Bob DSLによる型付きDBアクセス、gqlgenとRelayのコード生成、会計不変条件を守るトランザクション、リビジョンによる競合検知、Outboxの冪等性保証は維持します。コード行数ではなく、一つの機能を変更するときに読む必要がある範囲を減らすことを改善基準にします。

# Horse の保守性・シンプルさのレビュー

対象はHorseの`main`です。以前の指摘を現行コードと照合し、実装済みの内容を除いて残る課題を記録します。

## 1. 高優先：画面コンポーネントが大きい

主な画面ファイルは今も大きく、複数の業務機能と状態を一つのコンポーネントが所有しています。

| ファイル | 行数 | `useState`の呼び出し数 |
| --- | ---: | ---: |
| [settings-page.tsx](../../web/src/pages/settings/ui/settings-page.tsx) | 414 | 10 |
| [ledger-page.tsx](../../web/src/pages/ledger/ui/ledger-page.tsx) | 1,363 | 3 |
| [imports-page.tsx](../../web/src/pages/imports/ui/imports-page.tsx) | 865 | 8 |
| [purchase-create-page.tsx](../../web/src/pages/purchase-create/ui/purchase-create-page.tsx) | 497 | 10 |
| [settlement-page.tsx](../../web/src/pages/settlement/ui/settlement-page.tsx) | 596 | 19 |

`SettingsPage`は設定tab、カテゴリ作成、全体の未保存状態を管理し、口座設定、負担ルールと商品、タグ、予算、定期支出、入出金、招待、残高推移は同じ`pages/settings`sliceの専用UIやmodelへ分けました。各機能のGraphQL operationと型付きMutation APIも同sliceのAPI segmentへ移しました。`LedgerPage`は検索、一覧、購入編集、訂正、証拠、観測照合を扱い、品目一括編集の選択・適用・Undo状態を同じsliceのmodelとUIへ移しました。購入証拠の取得・表示はpage API・model・UIへ分離し、購入読み込みはEntity mapperを再利用します。検索filterのstyleはpage CSS moduleへ移しました。`ImportsPage`は取込一覧、確認、送信を扱い、撮影、画像確認、回転、切り抜き、証拠照合、明細確認は専用UIへ分けました。画像座標変換とURLに同期する検索条件は同じpage sliceのmodelにあります。`PurchaseCreatePage`は購入共通項目と保存・送信を持ち、品目、支払、詳細、負担配分の編集を同じsliceの専用UIとmodelへ移しました。`SettlementPage`は月次精算の組立てを担い、期間選択、貸付・返済、精算preview、締め確認は同じsliceの専用UIへ移しました。

各親ページには検索・一覧・画面遷移の調整が残ります。状態だけを巨大なHookへ移すのではなく、機能が独立して変更できるUIとmodelの境界を続けて整理します。

## 2. 高優先：Relay境界に手書き型と汎用変換が残っている

[household.ts](../../web/src/shared/api/household.ts)にはRelay生成型とは別の手書き型が残り、`executeGraphql()`と`useGraphqlMutation()`は`normalizeGraphQLValue()`を通します。PurchaseItemの汎用変換は削除しましたが、LedgerEntry変換と残りの再帰処理、Mutation結果のunion unwrapは共通APIにあります。

[use-household-purchases.ts](../../web/src/entities/purchase/api/use-household-purchases.ts)は生成されたPurchaseList fragmentの品目型を受け取る明示的なEntity mapperを使い、`as unknown as PurchaseItem[]`を解消しています。

購入作成・記帳と品目更新の変数変換はPurchase Entity内の生成型mapperへ移し、Mutation名による共通基盤の分岐と`as never`を削除しました。購入履歴、口座活動、残高一覧と履歴、配賦ルール、予算、タグ、定期支出、商品カタログ、商品候補、招待一覧、ホーム、分析、精算、未照合観測のread pathをRelay生成型と明示mapperへ移しました。招待、設定、精算のMutationもRelay生成型のvariablesと結果unionで処理します。帳簿の購入取得はPurchaseHistoryQueryのRelay生成型に移し、cursor変数をoperationへ明示します。画面の`readGraphql<T>`呼び出しを除去し、未使用の共有関数も削除しました。`executeGraphql()`にはアプリ起動時の家庭取得と招待操作が残り、`normalizeGraphQLValue()`によるLedgerEntry変換やMutationのUserError unwrapを維持しています。

## 3. 中優先：会計の共通規則が複数パッケージに重複している

イベント記録は`accounting.RecordEvent`へ集約し、購入・入金・振替・planningのlocal wrapperも削除しました。冪等性検証は操作ごとにfingerprintとresourceの契約が異なるため、共通化していません。

口座の初期残高、口座間移動、外部入金は`accounting.BuildEntries()`と`InsertEntries()`を使うように統一し、借貸行の生成と検証の重複を解消しました。

会計期間、符号付き整数のオーバーフロー検知、イベント記録は`accounting`へ集約済みです。冪等性検証の共通化は未対応です。

## 4. 中優先：画面固有CSSと重要な純粋処理が分離されていない

[global.css](../../web/src/app/styles/global.css)は4,392行あり、画面固有のスタイルも含みます。帳簿検索、精算、設定、購入作成の専用styleとスマートフォンのroot overscroll制御をページ単位へ移しました。共通デザイントークンと汎用UIを残し、ほかの画面固有規則も機能単位のCSSへ分ける余地があります。

画像座標の行列演算は[importsのmodel](../../web/src/pages/imports/model/evidence-geometry.ts)へ分離しました。`web/src`には`.test.ts`、`.test.tsx`、`.spec.ts`、`.spec.tsx`がなく、`web/package.json`にもテスト用スクリプトがありません。購入データ変換、Outbox、矩形範囲の処理など、純粋関数として分離できる重要処理の検証は未整備です。

## 5. 低優先：GraphQLの購入読み込みが広い関連データを組み立てる

[loadPurchases](../../internal/graph/helpers.go)は購入に対し、口座、証拠、確認、品目、支払、訂正、品目配賦、タグなどをまとめて読み込み、GraphQLモデルを構築します。Bobのバッチローダーを利用してN+1は避けていますが、一覧で一部のフィールドしか使わない場合にも広い関連データを読み込みます。

今すぐDataLoaderを追加する必要はありません。購入一覧と購入詳細で必要な読み取りデータが異なる箇所を確認し、負荷または変更頻度の問題が出る場合に一覧・詳細の読み取りモデルを分ける選択肢があります。読み取り量や応答時間は計測されていないため、現時点では性能問題としては扱いません。

## 改善の優先順位

| 優先 | 対象 |
| --- | --- |
| P1 | 大きなReact画面の機能・状態分割 |
| P1 | Relay生成型と明示的mapperを中心にした通信境界 |
| P2 | 操作ごとの冪等性検証の重複 |
| P2 | 画面固有CSSの分離と重要な純粋処理の検証 |
| P3 | 必要性を測定した後の購入読み取りモデル分割 |

Bob DSLによる型付きDBアクセス、gqlgenとRelayのコード生成、会計不変条件を守るトランザクション、リビジョンによる競合検知、Outboxの冪等性保証は維持します。コード行数ではなく、一つの機能を変更するときに読む必要がある範囲を減らすことを改善基準にします。

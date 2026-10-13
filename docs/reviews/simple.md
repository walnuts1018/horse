# Horse の保守性・シンプルさのレビュー

対象はHorseの`main`、コミット[`1433e4e`](https://github.com/walnuts1018/horse/commit/1433e4e)です。以前の指摘を現行コードと照合し、実装済みの内容を除いて残る課題を記録します。

## 1. 高優先：画面コンポーネントが大きい

主な画面ファイルは今も大きく、複数の業務機能と状態を一つのコンポーネントが所有しています。

| ファイル | 行数 | `useState`の呼び出し数 |
| --- | ---: | ---: |
| [settings-page.tsx](../../web/src/pages/settings/ui/settings-page.tsx) | 2,627 | 66 |
| [ledger-page.tsx](../../web/src/pages/ledger/ui/ledger-page.tsx) | 2,439 | 18 |
| [imports-page.tsx](../../web/src/pages/imports/ui/imports-page.tsx) | 2,556 | 20 |
| [purchase-create-page.tsx](../../web/src/pages/purchase-create/ui/purchase-create-page.tsx) | 1,218 | 12 |

`SettingsPage`は口座、負担先、配賦、商品、招待、タグ、予算、定期支出、資金移動、残高推移を扱います。`LedgerPage`は検索、一覧、選択、一括更新、購入編集、訂正、証拠、観測照合を扱います。`ImportsPage`は取込一覧、確認、送信、カメラ画像処理、座標変換を扱います。

画面の機能ごとにUIと状態の所有者を分け、親コンポーネントを配置と画面遷移の責務に限定する余地があります。単に処理を別ファイルや巨大なHookへ移すのではなく、機能が独立して変更できる境界を作る必要があります。

## 2. 高優先：Relay境界に手書き型と汎用変換が残っている

[household.ts](../../web/src/shared/api/household.ts)には、Relay生成型とは別の手書き型があり、`executeGraphql()`と`useGraphqlMutation()`の両方で`normalizeGraphQLValue()`を通します。`normalizePurchaseVariables()`はMutation名を調べて購入品目の入力形式を組み替えます。

[use-household-purchases.ts](../../web/src/entities/purchase/api/use-household-purchases.ts)もRelayの品目データを独自形式へ変換し、`as unknown as PurchaseItem[]`で型を合わせます。数量・単価のmapperは通信層とEntityの両方にあるため、スキーマと手書き型の不整合をコンパイル時に検知できず、変換責務も分散しています。

Relay生成型を通信境界で維持し、画面用の変換はEntityの明示的なmapperに置く必要があります。Mutation変数も操作名による汎用変換を使わず、生成された入力型から組み立てます。

## 3. 高優先：購入更新ServiceがGraphQL層の操作を受け取る

[internal/application/purchase/update.go](../../internal/application/purchase/update.go)の`Service.Update()`は、会計期間ロック、支払更新、仕訳同期を`UpdateOperations`として呼び出し元から受け取ります。[update_purchase.go](../../internal/graph/update_purchase.go)はGraphQL層にある`lockAccountingPeriods`、`replacePurchasePayments`、`syncPostedPurchaseAccounting`を渡します。購入更新のトランザクションと競合判定はApplication層にありますが、会計期間ロックと会計同期の実装を追うにはGraphQL層へ移動する必要があります。

購入品目更新も`UpdateItemOperations`に同様のDB操作を注入しています。購入訂正、品目更新、口座間移動、外部入金、予算・定期支出のDB処理自体はApplication Serviceへ移されているため、以前の「Resolverに取引処理が直接残る」という指摘は該当しません。残る課題は、購入更新系ServiceがGraphQL層の関数に依存する境界です。

会計期間ロック、支払更新、仕訳同期、商品別名更新をApplication層から直接利用できる内部処理へ移し、Resolverには認証・入力変換・Service呼び出し・結果変換を残す構成が候補です。

## 4. 高優先：購入登録がフロントエンドの複数Mutationに分かれている

[create-and-post.ts](../../web/src/entities/purchase/api/create-and-post.ts)は`createPurchase`の後に`postPurchase`を呼びます。記帳に失敗すると`createPurchase`を再実行して作成済み購入の状態を確認します。取り込み画面では購入登録後に証拠書類を別処理で関連付けます。

この経路ではクライアントが中間状態、再試行、部分成功を扱います。通常登録とAI取込で購入作成・記帳を一つのサーバーコマンドにまとめ、取込では証拠書類との関連も同じトランザクションに含めると、クライアント側の分岐を減らせます。明示的な下書き操作に使う既存Mutationは維持できます。

## 5. 中優先：会計の共通規則が複数パッケージに重複している

会計期間計算、金額加算、イベント記録、冪等性検証は購入、送金、資金拠出、口座移動など複数のApplicationパッケージに同様の関数があります。各処理の将来の変更で規則が分岐する可能性があります。

口座の初期残高は[mutations_account.go](../../internal/graph/mutations_account.go)、口座間移動と外部入金は[accountmovement/service.go](../../internal/application/accountmovement/service.go)で、仕訳行を個別に作成します。既存の`accounting.BuildEntries()`と`InsertEntries()`を使わずに借方・貸方行を組み立てるため、仕訳の検証規則も統一されていません。

共通化対象を会計期間、金額計算、イベント記録、冪等性、仕訳生成に限定し、既存の仕訳生成処理を各記帳経路から利用できるようにする余地があります。

## 6. 中優先：帳簿の検索条件がサーバー処理と画面処理に分かれている

購入検索では期間、検索語、分類、タグ、決済口座、負担先、起点、証拠有無をサーバー側で適用してからページングしています。全ページを無条件に取得してブラウザで絞るという以前の指摘は解消しています。

一方、[ledger-page.tsx](../../web/src/pages/ledger/ui/ledger-page.tsx)は購入表示と品目表示の検索条件を別々に実装し、サーバー検索後にも表示データを`filter()`と`flatMap()`で絞り込みます。金額の下限・上限はサーバーへ渡していないため、取得済みページ内だけに適用されます。フィルター型の一部は[ledger-search.ts](../../web/src/pages/ledger/model/ledger-search.ts)にありますが、Query変数への変換と画面側の絞り込みは分かれています。

検索条件を単一のQuery入力へまとめ、金額条件もページング前に適用したうえで、購入表示と品目表示で共通化できる判定を共有する必要があります。

## 7. 中優先：画面固有CSSと重要な純粋処理が分離されていない

[global.css](../../web/src/app/styles/global.css)は4,871行あり、画面固有のスタイルも含みます。共通デザイントークンと汎用UIを残し、画面に固有の規則を機能単位のCSSへ分ける余地があります。

`web/src`には`.test.ts`、`.test.tsx`、`.spec.ts`、`.spec.tsx`がなく、`web/package.json`にもテスト用スクリプトがありません。特に[imports-page.tsx](../../web/src/pages/imports/ui/imports-page.tsx)には画像座標の行列演算と範囲変換が残っています。購入データ変換、Outbox、選択範囲、座標変換など、純粋関数として分離できる重要処理の検証がありません。

## 8. 低優先：GraphQLの購入読み込みが広い関連データを組み立てる

[loadPurchases](../../internal/graph/helpers.go)は購入に対し、口座、証拠、確認、品目、支払、訂正、品目配賦、タグなどをまとめて読み込み、GraphQLモデルを構築します。Bobのバッチローダーを利用してN+1は避けていますが、一覧で一部のフィールドしか使わない場合にも広い関連データを読み込みます。

今すぐDataLoaderを追加する必要はありません。購入一覧と購入詳細で必要な読み取りデータが異なる箇所を確認し、負荷または変更頻度の問題が出る場合に一覧・詳細の読み取りモデルを分ける選択肢があります。読み取り量や応答時間は計測されていないため、現時点では性能問題としては扱いません。

## 改善の優先順位

| 優先 | 対象 |
| --- | --- |
| P1 | 大きなReact画面の機能・状態分割 |
| P1 | Relay生成型と明示的mapperを中心にした通信境界 |
| P1 | 購入更新ServiceからGraphQL層の操作注入を解消 |
| P1 | 購入作成・記帳のサーバー側集約 |
| P2 | 会計期間、金額計算、イベント、冪等性、仕訳生成の共通化 |
| P2 | 帳簿フィルターのQuery入力と画面表示の統合 |
| P2 | 画面固有CSSの分離と重要な純粋処理の検証 |
| P3 | 必要性を測定した後の購入読み取りモデル分割 |

Bob DSLによる型付きDBアクセス、gqlgenとRelayのコード生成、会計不変条件を守るトランザクション、リビジョンによる競合検知、Outboxの冪等性保証は維持します。コード行数ではなく、一つの機能を変更するときに読む必要がある範囲を減らすことを改善基準にします。

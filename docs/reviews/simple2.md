# Horse コードレビュー

対象: [walnuts1018/horse](https://github.com/walnuts1018/horse)、`main` のコミット [`b80a58f`](https://github.com/walnuts1018/horse/commit/b80a58f8de5cc072cffc8517630ce009c97d0a93)

最も効果が大きいのは、既存ライブラリで処理できることを独自実装しないことと、重複した取得・更新処理を整理することです。

フロントエンドだけでなく、GoのAPI、GraphQL、取り込み処理まで確認しました。会計上の不変条件やトランザクション処理は、単にコードを短くするために削るべきではありません。

## 優先度の高い改善点

### 1. P1: 文書一覧のN+1クエリを解消する

対象: [`internal/api/documents.go`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/internal/api/documents.go#L614-L740)

`documents()`では、一覧に含まれる文書を1件ずつ処理する中で、次のDB取得を繰り返しています。

- `documentResponse()`：最新の解析ジョブを文書ごとに検索
- `loadDocumentAttachments()`：添付関係、添付先文書、ジョブ、親文書を個別取得
- `loadDocumentConfirmations()`：確定結果を文書ごとに検索し、訂正も個別取得

50件の一覧なら、添付がなくても多数のクエリが発生する構造です。一方、証拠リンクは既に一括取得できています。

改善方法: 一覧の文書IDを収集し、最新ジョブ・添付関係・確認結果をそれぞれ一括取得して、Goの`map[UUID]...`にまとめます。最後に文書レスポンスを組み立てる構成にすると、SQL実行回数もコードの責務も明確になります。

なお、一覧と単一文書取得は同じ組み立て処理を利用できます。HTTPハンドラー側は認証、検索条件、レスポンス返却だけに絞るのがよいです。

### 2. P1: 購入訂正履歴の二重ページングをやめる

対象: [`internal/graph/helpers.go`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/internal/graph/helpers.go#L288-L505)、[`query_purchases.go`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/internal/graph/query_purchases.go#L293-L347)、[`use-household-purchases.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/entities/purchase/api/use-household-purchases.ts#L141-L188)

サーバーは`loadPurchases()`で全訂正履歴を読み込み、GraphQL Resolverでメモリ上の配列をページ分割しています。さらにフロントでは、100件を超えた分を`fetchQuery`のループで取り直しています。

つまり、DBで全件取得 → APIでページ分割 → クライアントが全ページ再取得という遠回りな構成です。

改善するなら、一覧に必要な訂正集計と、購入詳細に必要な訂正履歴を分離します。訂正履歴自体はDBでページングし、RelayのConnectionで取得してください。

返品可能額の計算も、フロントで全訂正履歴を集めるより、サーバーから`remainingRefundableAmountYen`のような計算済み値を返す設計が適しています。

これでフロントの`completeCorrections`、専用の取得Effect、カーソルループをまとめて削除できます。

### 3. P1: データ取得のための`useEffect`と独自stateを減らす

対象: [`use-analysis-data.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/analysis/model/use-analysis-data.ts)、[`home-page.tsx`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/home/ui/home-page.tsx)、[`use-unmatched-observations.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/ledger/api/observation/use-unmatched-observations.ts)

Analysisには4つ、Homeには3つの取得Effectがあります。loading、error、取得結果、キャンセル判定まで各所で管理しており、ReactとRelayが本来提供する仕組みを重複して実装しています。

特に`useUnmatchedObservations()`は、取得・ページングだけで複数のstateとrefを管理しています。

改善方針は、取得元に合わせてライブラリを統一することです。

GraphQLはRelayの`useLazyLoadQuery`、`usePreloadedQuery`、`usePaginationFragment`を基本にします。HomeやAnalysisは画面単位のGraphQL operationへの統合も検討してください。既に適切に使われているImportsのREST APIについては、TanStack Queryを維持します。

また、各所の`queueMicrotask(() => setState(...))`はEffectのlintを回避するために使われていますが、複雑な非同期状態管理の根本的な解決にはなっていません。データ取得用のEffectそのものをなくす方向がよいです。

### 4. P2: GraphQL Mutationの共通処理を統一する

対象: [`category-management.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/settings/api/category-management.ts)、[`imports/api/documents.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/imports/api/documents.ts)、[`commit-mutation.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/settings/api/commit-mutation.ts)

カテゴリ更新では3種類のMutationがほぼ同じ`new Promise`と`commitMutation`、`switch (__typename)`を繰り返しています。Importsにも同様の実装が5種類あります。

一方、Settingsの他の機能には`commitSettingsMutation()`という共通処理が既に存在します。

改善方法: 既存の共通処理を`shared/api`へ移し、Mutation実行とエラー変換を統一します。成功結果の読み取りだけ、型付きコールバックとして各Mutation側に残します。

ただし、全Mutationを巨大な設定オブジェクトに押し込む必要はありません。呼び出し側が`createCategory(input)`のように読めるAPIは維持する方が簡潔です。

### 5. P2: `App`に集中している責務を分離する

対象: [`horse-app.tsx`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/app/horse-app.tsx)

約930行のファイルで、認証、世帯切り替え、招待受諾、ページナビゲーション、未保存変更のブロック、オフライン送信処理、通知表示まで管理しています。

特に`App()`の中には多数の`useRef`、`useEffect`、コールバックがあり、変更の影響範囲を追いにくくしています。

ここは小さな関数に機械的に分割するより、機能単位に責務を分けるのがよいです。

`useHouseholdSession`で認証と世帯取得、`usePurchaseOutboxSender`で送信処理、`usePendingInvitation`で招待処理を扱い、`AppShell`にはナビゲーションと表示を残す構成を推奨します。

認証・世帯データの取得は、TanStack Routerのloaderへの移行も有力です。ただしSSRを使う場合、Relay Environmentをサーバープロセスで共有せず、リクエスト単位に分離する設計が必要です。

### 6. P2: 取り込み確定処理を純粋関数と実行処理に分ける

対象: [`use-import-confirmation.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/imports/model/use-import-confirmation.ts)

約620行のHookが、レシート編集、支払口座の推定、金額確認、購入登録、銀行明細、返金、振替、貸付、返済などの処理を担当しています。

この部分はドメイン処理なので、無理に統合すると逆に読みにくくなります。

そこで、入力検証とMutation用payload生成を純粋関数として切り出し、Hookにはフォーム状態と非同期処理の調整だけを残すことを推奨します。例えばレシート確定と明細取引確定は別モジュールにできます。

現在のテストは補助関数中心なので、分離した金額計算・返金・複数支払の入力生成も直接テストできるようにすると改善効果が大きいです。

### 7. P2: IndexedDBの定型コードを`idb`へ置き換える

対象: [`purchase-storage.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/shared/lib/purchase-storage.ts)

約720行の中に`IDBRequest`の`onsuccess`、`transaction.oncomplete`、`onerror`などのPromise化が何度も登場します。

ここは新しいライブラリを導入する価値があります。

`idb`を推奨します。 ブラウザー標準IndexedDBの軽量なPromiseラッパーで、現在のDBスキーマやトランザクションを基本的に維持したまま、定型コードを削減できます。

ただし、購入送信のlease、冪等性キー、複数タブ間の排他、下書きとOutboxの同時更新は重要です。これらの原子性は維持し、`tx.done`でコミット完了を確認する実装にします。

### 8. P3: Settingsのタブ状態をURLへ一本化する

対象: [`settings-navigation.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/settings/model/settings-navigation.ts)

タブ選択を`useState`で管理し、hashの変更を`useSyncExternalStore`とEffectで反映しています。

TanStack Routerのsearch paramsを唯一の状態にすれば、`useState`、hash購読、同期Effectが不要になります。既存のセットアップ用hashリンクだけ互換処理で引き継げば十分です。

## 実施順序

| 順位 | 作業 | 主な効果 |
| -- | ----------------------- | ------------------------ |
| 1 | 文書一覧の一括DB取得 | N+1解消、責務整理 |
| 2 | 訂正履歴の取得設計変更 | 二重ページングと不要な状態を削除 |
| 3 | Relayによる取得状態の統一 | `useEffect`と独自stateの大幅削減 |
| 4 | Mutation共通処理の統一 | 重複コード削減 |
| 5 | Appと取り込みHookの責務分離 | 変更時の見通しを改善 |
| 6 | `idb`導入、SettingsのURL状態化 | ブラウザー側の定型コード削減 |

## 維持したい実装

Bobの型付きクエリ、gqlgenとRelayのコード生成、会計計算の整数処理、購入訂正の履歴、リビジョンによる競合検出、Outboxの冪等性と原子的な保存は残すべきです。

また、Goの大きなファイルを単に小さなファイルへ分割するだけでは、実質的な複雑さは減りません。優先すべきなのは、今回の文書取得や訂正履歴のように、処理の流れ自体を簡潔にできる箇所です。

今回のレビューはGitHub上の現行ソースを対象とした静的レビューで、コード変更やテスト実行は行っていません。

総合的には、アーキテクチャを作り直す必要はありません。RelayとTanStack Queryに状態管理を任せ、重複したAPI処理をまとめ、複雑なドメイン処理だけを明示的に残す方針が最も効果的だと考えます。

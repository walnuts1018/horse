# Horse コード・アーキテクチャの未解決項目

対象: [walnuts1018/horse](https://github.com/walnuts1018/horse)、`main` のコミット [`b80a58f`](https://github.com/walnuts1018/horse/commit/b80a58f8de5cc072cffc8517630ce009c97d0a93)

## 優先度の高い改善点

### 1. P2: GraphQL Mutationの共通処理を統一する

対象: [`category-management.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/settings/api/category-management.ts)、[`imports/api/documents.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/imports/api/documents.ts)、[`commit-mutation.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/settings/api/commit-mutation.ts)

カテゴリ更新では3種類のMutationがほぼ同じ`new Promise`と`commitMutation`、`switch (__typename)`を繰り返しています。Importsにも同様の実装が5種類あります。

一方、Settingsの他の機能には`commitSettingsMutation()`という共通処理が既に存在します。

改善方法: 既存の共通処理を`shared/api`へ移し、Mutation実行とエラー変換を統一します。成功結果の読み取りだけ、型付きコールバックとして各Mutation側に残します。

ただし、全Mutationを巨大な設定オブジェクトに押し込む必要はありません。呼び出し側が`createCategory(input)`のように読めるAPIは維持する方が簡潔です。

### 2. P2: `App`に集中している責務を分離する

対象: [`horse-app.tsx`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/app/horse-app.tsx)（認証・世帯一覧・世帯詳細取得の`240-259,267-295`行目含む）

約930行のファイルで、認証、世帯切り替え、招待受諾、ページナビゲーション、未保存変更のブロック、オフライン送信処理、通知表示まで管理しています。

特に`App()`の中には多数の`useRef`、`useEffect`、コールバックがあり、変更の影響範囲を追いにくくしています。

ここは小さな関数に機械的に分割するより、機能単位に責務を分けるのがよいです。

`useHouseholdSession`で認証と世帯取得、`usePurchaseOutboxSender`で送信処理、`usePendingInvitation`で招待処理を扱い、`AppShell`にはナビゲーションと表示を残す構成を推奨します。

認証・世帯データの取得は、TanStack Routerのloaderへの移行も有力です。ただしSSRを使う場合、Relay Environmentをサーバープロセスで共有せず、リクエスト単位に分離する設計が必要です。

### 3. P2: 取り込み確定処理を純粋関数と実行処理に分ける

対象: [`use-import-confirmation.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/imports/model/use-import-confirmation.ts)

約620行のHookが、レシート編集、支払口座の推定、金額確認、購入登録、銀行明細、返金、振替、貸付、返済などの処理を担当しています。

この部分はドメイン処理なので、無理に統合すると逆に読みにくくなります。

そこで、入力検証とMutation用payload生成を純粋関数として切り出し、Hookにはフォーム状態と非同期処理の調整だけを残すことを推奨します。例えばレシート確定と明細取引確定は別モジュールにできます。

現在のテストは補助関数中心なので、分離した金額計算・返金・複数支払の入力生成も直接テストできるようにすると改善効果が大きいです。

### 4. P2: IndexedDBの定型コードを`idb`へ置き換える

対象: [`purchase-storage.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/shared/lib/purchase-storage.ts)

約720行の中に`IDBRequest`の`onsuccess`、`transaction.oncomplete`、`onerror`などのPromise化が何度も登場します。

ここは新しいライブラリを導入する価値があります。

`idb`を推奨します。ブラウザー標準IndexedDBの軽量なPromiseラッパーで、現在のDBスキーマやトランザクションを基本的に維持したまま、定型コードを削減できます。

ただし、購入送信のlease、冪等性キー、複数タブ間の排他、下書きとOutboxの同時更新は重要です。これらの原子性は維持し、`tx.done`でコミット完了を確認する実装にします。

### 5. P2: ブラウザー離脱時の未保存確認

対象: [`horse-app.tsx:176-185`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/app/horse-app.tsx#L176-L185)

画面遷移は`useBlocker`で確認し、ブラウザー離脱には`enableBeforeUnload`を設定しています。しかし公開環境で購入下書きから直接再読み込みした際に確認が表示されなかった記録があるため、ブラウザーでの更新・タブ終了時の動作は未解決です。

### 6. P2: 商品catalogの取得とRelay cache利用

対象: [`use-ledger-purchase-editor.ts:65-73`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/ledger/model/use-ledger-purchase-editor.ts#L65-L73)、[`product-variant-select.tsx:32-47`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/imports/ui/product-variant-select.tsx#L32-L47)、[`read-product-catalog.ts:17-31`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/entities/product/api/read-product-catalog.ts#L17-L31)、[`read-product-suggestions.ts:28-42`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/entities/product/api/read-product-suggestions.ts#L28-L42)

商品catalogは`useLedgerPurchaseEditor`のEffectから全ページを`fetchQuery`で読み込み、候補検索も入力ごとに`fetchQuery`しています。両queryは`network-only`のため、再表示時に取得を繰り返します。Relayのquery cacheを再利用する読み込み方法へ整理してください。

### 7. P3: Settingsのタブ状態をURLへ一本化する

対象: [`settings-navigation.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/settings/model/settings-navigation.ts)

タブ選択を`useState`で管理し、hashの変更を`useSyncExternalStore`とEffectで反映しています。

TanStack Routerのsearch paramsを唯一の状態にすれば、`useState`、hash購読、同期Effectが不要になります。既存のセットアップ用hashリンクだけ互換処理で引き継げば十分です。

## 実施順序

| 順位 | 作業 | 主な効果 |
| -- | ----------------------- | ------------------------ |
| 1 | Mutation共通処理の統一 | 重複コード削減 |
| 2 | Appと取り込みHookの責務分離 | 変更時の見通しを改善 |
| 3 | 商品catalogのcache利用・未保存確認修正 | 不要な取得と誤離脱の防止 |
| 4 | `idb`導入、SettingsのURL状態化 | ブラウザー側の定型コード削減 |

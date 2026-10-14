# Horse Frontend Reactの未解決項目

現行の`web/src`を確認し、解決済みの指摘を除きました。購入履歴と帳簿一覧はRelayのConnectionとpaginationを使い、Importsの一覧・詳細・プレビューはTanStack Queryを使います。画面のpathはTanStack Routerのfile-based routingを使い、Ledger、Imports、Settingsの選択状態はURLに保存します。

## ブラウザー離脱時の未保存確認

画面遷移は`useBlocker`で確認し、ブラウザー離脱には`enableBeforeUnload`を設定しています（`web/src/app/horse-app.tsx:176-185`）。公開環境で購入下書きから直接再読み込みした際に確認が表示されなかった記録があるため、ブラウザーでの更新・タブ終了時の動作は未解決です。

## 商品catalogの取得とRelay cache利用

商品catalogは`useLedgerPurchaseEditor`のEffectから全ページを`fetchQuery`で読み込み、候補検索も入力ごとに`fetchQuery`します（`web/src/pages/ledger/model/use-ledger-purchase-editor.ts:65-73`、`web/src/pages/imports/ui/product-variant-select.tsx:32-47`、`web/src/entities/product/api/read-product-catalog.ts:17-31`、`web/src/entities/product/api/read-product-suggestions.ts:28-42`）。両queryは`network-only`のため、再表示時に取得を繰り返します。Relayのquery cacheを再利用する読み込み方法へ整理してください。

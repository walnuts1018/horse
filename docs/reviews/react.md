# Horse Frontend Reactの未解決項目

現行の`web/src`を確認し、解決済みの指摘を除きました。購入履歴と帳簿一覧はRelayのConnectionとpaginationを使い、Importsの一覧・詳細・プレビューはTanStack Queryを使います。画面のpathはTanStack Routerのfile-based routingを使い、LedgerとImportsのfilterはURLに保存します。

## ブラウザー離脱時の未保存確認

画面遷移は`useBlocker`で確認し、ブラウザー離脱には`enableBeforeUnload`を設定しています（`web/src/app/horse-app.tsx:176-185`）。公開環境で購入下書きから直接再読み込みした際に確認が表示されなかった記録があるため、ブラウザーでの更新・タブ終了時の動作は未解決です。

## Relayによるページ取得とcache利用

購入一覧の全ページ読み込みは`loadAll`に応じてEffectから`loadNext`を繰り返し、訂正履歴もEffect内で追加の`fetchQuery`とcursor loopを行います（`web/src/entities/purchase/api/use-household-purchases.ts:141-188,216-221`）。全件取得が必要な画面では、一覧paginationと訂正履歴の取得範囲を見直してください。

未照合観測はRelay Connectionを`fetchQuery`で取得した後、cursor、request generation、loading、errorを独自stateで管理しています（`web/src/pages/ledger/api/observation/use-unmatched-observations.ts:27-46,48-171`）。Relayの`usePaginationFragment`を使い、Connectionとloading stateをRelayに寄せる余地があります。

商品catalogは`useLedgerPurchaseEditor`のEffectから全ページを`fetchQuery`で読み込み、候補検索も入力ごとに`fetchQuery`します（`web/src/pages/ledger/model/use-ledger-purchase-editor.ts:65-73`、`web/src/pages/imports/ui/product-variant-select.tsx:32-47`、`web/src/entities/product/api/read-product-catalog.ts:17-31`、`web/src/entities/product/api/read-product-suggestions.ts:28-42`）。両queryは`network-only`のため、再表示時に取得を繰り返します。Relayのquery cacheを再利用する読み込み方法へ整理してください。

Homeはsummary、月間支出、精算を3つのEffectで別々に取得し、Analysisも支出、メンバー別支出、予算、定期支出を別々のEffectで取得します（`web/src/pages/home/ui/home-page.tsx:41-72`、`web/src/pages/analysis/model/use-analysis-data.ts:53-129`）。どちらもRelay operationの結果を画面local stateで保持しているため、再訪問時のquery再利用と複数データのloading/error管理が分かれています。Relayのquery hookとcacheを使う単位を整理してください。

## RouterのloaderとSettingsのURL状態

認証情報と世帯一覧は`horse-app.tsx`のmount後に取得し、世帯詳細も別Effectで取得しています（`web/src/app/horse-app.tsx:240-259,267-295`）。TanStack Routerの`beforeLoad`や`loader`を使っていないため、初期表示で認証・世帯一覧・世帯詳細の取得が続きます。route contextとloaderで依存関係を整理してください。

Settingsのtab選択は`useState`で保持し、URLへ書き込みません（`web/src/pages/settings/model/settings-navigation.ts:28-43`）。再読み込みや共有URLからtabを復元する必要があるため、TanStack Routerのsearch stateへ移してください。

## 帳簿のEvidence highlight

帳簿の通常の展開buttonはhighlightをresetしますが、「購入を開く」操作は展開対象だけを更新します（`web/src/pages/ledger/ui/ledger-page.tsx:367-373,434-440`）。highlight IDが別の購入へ残らないよう、展開対象のIDと対応付けてください。

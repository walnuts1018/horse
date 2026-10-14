# Horseの追加レビューで未解決の項目

## 1. 購入Subscriptionの変更通知

`web/src/entities/purchase/api/use-household-purchases.ts`は品名だけの変更を対象のRelay nodeへ反映し、訂正時は対象購入の訂正履歴だけを破棄する。その他の変更は100ms以内の通知をまとめて購入一覧全体を再取得するため、一覧全体の更新を避けられていない。

変更イベントの内容だけで対象購入を更新できる種類を増やし、検索結果や並び順に影響する変更だけ一覧を再取得する。

## 2. SubscriptionごとのDBポーリング

`internal/graph/purchase_subscription.resolvers.go`と`internal/graph/shared_purchase_draft_subscription.resolvers.go`は接続ごとに1秒間隔でDBを確認する。同時接続数に比例して所属確認とイベント検索が増える。

DB通知などの起床手段を使い、権限の再確認には上限付きの定期確認を残す。

## 3. OCRサービスのCIテスト

`.github/workflows/ci.yaml`はOCRサービスのLintを実行するが、`services/ocr/test_app.py`のテストを実行していない。CIで依存を再現可能に導入し、OCRサービスのテストも実行する。

## 4. 照合に使う取引IDの発行元

`internal/ingestion/reconcile.go`は`TransactionID`の一致を強く評価するが、取引IDの発行元を比較しない。異なる決済事業者が同じIDを発行した場合、照合候補の順位を誤る可能性がある。

取引IDを発行元と組にして比較する。現在は候補提示のみで自動統合しないため、誤った候補が確定データを直接変更することはない。

## 5. 分析画面を開いたまま日付が変わる場合

分析画面は`today`をstateの初期値として一度計算する。画面を開いたまま日付が変わると、定期支出の有効期間判定に古い日付を使う。

日付境界で判定を更新するか、日付を読む計算を画面表示時に再実行する。

## 6. 購入品目価格履歴のソート

`web/src/pages/analysis/ui/item-price-panel.tsx`の`sortBy`は挿入ソートを独自実装している。標準の`toSorted`を使えば同じ順序を保ったまま独自アルゴリズムをなくせる。

## 7. 不正なUUIDをNil UUIDへ変換するヘルパー

`internal/graph/mutation_support.go`の`generatedUUID`は文字列のUUID変換エラーを無視し、Nil UUIDを返す。呼び出し元の不正IDを通常のIDとしてDBへ渡す経路が生じる。

エラーを呼び出し元へ返すか、妥当なUUIDだけを受け取る型へ変更する。

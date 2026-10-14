# Horseの追加レビューで未解決の項目

## 1. 購入Subscriptionの変更通知

`web/src/entities/purchase/api/use-household-purchases.ts`は品名だけの変更を対象のRelay nodeへ反映し、訂正時は対象購入の訂正履歴だけを破棄する。その他の変更は100ms以内の通知をまとめて購入一覧全体を再取得するため、一覧全体の更新を避けられていない。

変更イベントの内容だけで対象購入を更新できる種類を増やし、検索結果や並び順に影響する変更だけ一覧を再取得する。

## 2. SubscriptionごとのDBポーリング

`internal/graph/purchase_subscription.resolvers.go`と`internal/graph/shared_purchase_draft_subscription.resolvers.go`は接続ごとに1秒間隔でDBを確認する。同時接続数に比例して所属確認とイベント検索が増える。

DB通知などの起床手段を使い、権限の再確認には上限付きの定期確認を残す。

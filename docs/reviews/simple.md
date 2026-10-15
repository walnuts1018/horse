# 未解決のBEレビュー

## Purchaseの未要求field取得

[`loadPurchases()`](../../internal/graph/helpers.go)はGraphQLの選択fieldにかかわらずPurchaseItem、PurchaseCorrection、PurchaseItemCorrectionを取得し、表示モデルを組み立てます。`items`、`totalYen`、訂正関連fieldを選択しない購入一覧でも同じ取得が発生するため、fieldごとに必要なaggregateと明細を判定して読み込みを省く必要があります。

## Settlement Nodeのopen preview

[`loadSettlementNodes()`](../../internal/graph/nodes_settlement.go)はclosed runをまとめて読み込みますが、closed runがないSettlement Nodeごとに`settlementapp.New(r.DB).Get()`を呼び出します。異なるhousehold・periodのopen Nodeを一度に解決するとpreview用DBアクセスがNode数に応じて増えるため、複数Nodeを一括計算する経路が必要です。

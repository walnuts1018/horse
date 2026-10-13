## Relayページ取得API

`Budget`、`Tag`、`RecurringExpense`、`ProductCatalog`の一覧取得は`hasNextPage`を見てread関数内で最後まで取得する。設定画面など全件が必要な用途でも、RelayのConnectionとStoreを通して取得する構成へ揃えるか検討する。全件表示が必要な箇所と初期ページだけでよい箇所を特定してから、追加読込のUIと既存の並び順を決める。

## 商品属性のnullと空文字

`ProductVariant.brand`と`packageSize`はDB列がNOT NULLで空文字を保存する一方、APIの一部mapperは空文字をnullとして返し、batch resolverには空文字を返す経路がある。未設定値をnullと空文字のどちらで表すかAPI契約を決め、すべてのresolverを揃える。

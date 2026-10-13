# Horse実装レビュー（2026年10月13日）

## 商品候補検索の性能計測

[`internal/postgres/query/product_suggestions.go`](../../internal/postgres/query/product_suggestions.go)は完全一致・前方一致の候補を部分一致候補より先に取得し、部分一致候補を最大500件に制限します。関連する商品と別名をまとめて読み込み、候補の順位付けはGo側で行います。部分一致検索は先頭ワイルドカードを使うため、通常のB-tree索引を利用できません。

部分一致検索の実データ量に対する応答時間とDB読取量は計測していません。実測で遅延が確認された場合に、PostgreSQLのtrigram索引とDB側ランキングを検討します。

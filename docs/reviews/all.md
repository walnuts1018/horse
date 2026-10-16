# 未解決のコードレビュー指摘

## 購入履歴による品目候補

現在は直近20件の購入から候補を取得し、OCR文字列の部分一致で選択しています。

履歴を利用する機能には、候補の取得期間と検索方法に制約があります。購入履歴からは直近20件の購入しか取得しません。

```
.order((purchases::occurred_at.desc(), purchases::id.desc()))
.select(purchases::id)
.limit(20)
```

さらに、候補として採用する条件が、OCRが認識した文字列との部分一致です。

```
if (printed_key.is_empty()
    || !source_text.contains(&printed_key))
    && (normalized_key.is_empty()
        || !source_text.contains(&normalized_key))
{
    continue;
}
```

この方式では、数か月前によく購入していた商品や、表記揺れが大きい商品を発見しにくくなります。

また、OCRで1行も認識できなかった場合は、Geminiが原本画像を正しく読めていても履歴候補を一切利用しません。

過去履歴による補完が、補助的なOCRの認識結果に依存しているのは改善の余地があります。

改善案

履歴検索は、OCRの文字列から直接選ぶ方式だけに依存せず、画像から抽出した商品名・店舗・数量・金額を利用する二段階方式にします。

まず、Geminiが原本から観測値を抽出します。その後、PostgreSQLに蓄積したすべての購入履歴と商品マスタから関連候補を検索します。

検索時には、完全一致、別名、店舗、数量、容量、価格帯、購入頻度などを利用します。

候補が複数残った場合だけLLMに問い合わせれば、膨大な購入履歴をプロンプトに渡さなくても、認識品質を改善できます。



## Adapterのモジュール分割

次のRust Adapterは複数の責務を1ファイルに集めています。

| ファイル | 行数 |
| --- | ---: |
| `adapters/http/src/lib.rs` | 4,857 |
| `adapters/worker-runtime/src/processing.rs` | 3,767 |
| `adapters/graphql/src/lib.rs` | 1,437 |
| `adapters/graphql/src/purchase_mutations.rs` | 1,177 |

HTTPのOIDC認証、文書登録、照合、AI会話、イベント配信を責務別のmoduleへ分けます。Workerのモデル選択、quota、prompt、OCR、税検証、履歴検索、SSE処理もそれぞれ既存crate内のmoduleへ分けます。GraphQLのroot queryとmutationはdomainごとに分割します。

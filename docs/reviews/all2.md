## Horse 現時点のコードレビュー

[walnuts1018/horse](https://github.com/walnuts1018/horse) を確認しました。全体として、会計台帳、複数決済、負担ルールの履歴保存、冪等性、家庭単位の認可は、初期実装としてかなり整理されています。アーキテクチャを全面的に作り直す必要はないと考えます。

ただし、すでに実装されている処理にも、金額の意味が変わってしまう不具合と、AIによる抽出結果を誤って正常と判断しかねない箇所があります。未実装機能の列挙ではなく、今のコードをベースに優先順位を付けて指摘します。

調査中にも `main` にコミットが追加されていたため、レビュー対象を `345d605` に固定しました。主要ロジックはその直前の `9ba899e` から変更されていないことを差分で確認しています。静的レビューであり、今回新たにDB統合テストや実ブラウザ操作を実行したわけではありません。

### P1 — 返品・返金が支出として登録される可能性がある

[use-import-confirmation.ts:291–316](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/web/src/pages/imports/model/use-import-confirmation.ts#L291-L316)

カード明細の確定処理で、金額に `Math.abs(entry.amount)` を使っています。

```
paymentAllocations: paymentAccountId
  ? [{ paymentAccountId, amountYen: Math.abs(entry.amount) }]
  : [],
items: [{
  name: entry.merchant || "カード利用",
  amountYen: Math.abs(entry.amount),
  allocations: null,
}],
```

例えば、カード明細に返品の -3,000円 が記録され、取引種別が `CARD_PURCHASE` になっていた場合、Horseでは +3,000円の支出として確定されます。収支が実際と逆転するため、これは単なる表示上の問題ではありません。

さらに、[DBの](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/migrations/20261012000000_core.sql#L88-L103)[`purchase_items.amount_yen`](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/migrations/20261012000000_core.sql#L88-L103) は負額を禁止し、[購入確定処理](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/internal/application/purchase/post.go#L102-L111) も購入合計が正であることを要求します。一方、[AIのレシート検証](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/internal/ingestion/receipt.go) は負額の返品を扱う設計です。

つまり、抽出層は返品を理解できるのに、記帳層は返品を正しく表現できない状態です。

修正方針: `Math.abs` で符号を消すのをやめ、返品・返金・購入取消を会計上の独立した取引として扱えるようにしてください。元購入への関連付け、口座残高、負担配賦、月次支出、締め後訂正への影響まで一貫して定義する必要があります。

少なくとも、負額を正の支出へ変換するくらいなら、未対応の返品として確定を拒否した方が安全です。

### P1 — 購入日が不明なレシートを、今日の支出として確定してしまう

[use-import-confirmation.ts:266–284](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/web/src/pages/imports/model/use-import-confirmation.ts#L266-L284)

```
occurredAt: receipt?.purchasedAt
  ? new Date(receipt.purchasedAt).toISOString()
  : new Date().toISOString(),
```

AIは購入日が分からない場合、`purchasedAt` を空にするよう指示されています。しかしUIで確定すると、その値が現在日時に置き換わります。

例えば、2025年のレシートを2026年10月にまとめて取り込んだ場合、日付が判読できないだけで2026年10月の支出になります。Horseは月次精算・締め処理を持つため、集計結果や負担額まで変わります。

修正方針: 日付未確定の状態を維持し、ユーザーが購入日を明示的に入力するまで確定できないようにしてください。初期入力候補としてアップロード日を表示することはできますが、確定した購入日と混同してはいけません。

### P1 — PDFのレシートは独立OCRとの照合を通らず、正常判定され得る

[worker_extract.go:38–76](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/internal/ingestion/worker_extract.go#L38-L76)

現状、独立OCRの実行条件は `image/` で始まるMIMEタイプのみです。

```
if strings.HasPrefix(document.MIMEType, "image/") {
    lines, ocrErr = w.OCR.Extract(ctx, document)
}
```

その後の `CompareOCR` も画像にしか適用されません。PDFはGeminiの読み取りと、その結果自身の金額計算だけで `Balanced=true` になり得ます。

例えばGeminiが「498円」を「488円」と誤読し、誤読した明細だけで合計も整合していた場合、計算上の一致では誤りを検出できません。画像ではOCRとの不一致をレビュー事項に残せるのに、PDFでは同じ品質保証がありません。

修正方針: PDFをページごとに画像化して独立OCRにかけ、原本座標へ戻したうえでGeminiの品名・金額・行数と比較してください。少なくとも、`Balanced` を「算術的に整合している」と「原本との照合まで済んでいる」の二つに分けるべきです。

`Balanced=true` という一つの値に両方の意味を持たせるのは、今後の自動確定機能でも危険です。

### P2 — 注文メールに添付された領収書・請求書を読み取れない

[email.go:91–95](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/internal/ingestion/email.go#L91-L95) および [114–130](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/internal/ingestion/email.go#L114-L130)

メールパーサーは添付ファイルを検出しますが、現在は読み捨てて `HasUnparsedAttachments` を設定するだけです。

本文に注文情報があれば警告を残して処理できます。しかし、購入明細が添付PDFにしかないメールは情報不足になります。本文が実質空なら `email has no readable text body` で抽出自体が失敗します。

これはコードが壊れているというより、目的に対して処理がまだ不足している部分です。

修正方針: MIMEパーサーが添付PDF・画像を取り出し、元メールとの親子関係を保持して、文書ごとに抽出・照合できるようにしてください。複数の添付を一つの購入に無条件でまとめず、同一注文かどうかも照合対象にします。

### P2 — 複数取引を含むPDF・画像明細を、単一のレシートとして処理している

[worker_extract.go:15–51](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/internal/ingestion/worker_extract.go#L15-L51)

カード・銀行明細を複数の独立した取引として抽出するのは、現在 `text/csv` に限られています。それ以外のファイルは、複数取引を含んでいても単一の `Receipt` 形式でGeminiに渡されます。

さらに、[アップロード時のファイル種別判定](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/web/src/pages/imports/model/imports.ts#L145-L159) はCSV以外を原則レシートに分類するため、PDFのカード明細を適切な取引種別で取り込むUIも不足しています。

これは、レシートと明細のデータモデルを分離すれば解決できます。

| 入力 | 適切な抽出結果 |
| ----------- | --------------- |
| レシート・請求書 | 1件の購入と複数品目 |
| 注文メール | 注文・発送・支払の観測情報 |
| カード明細PDF・画像 | 独立した複数のカード取引 |
| 銀行明細PDF・画像 | 独立した複数の入出金・振替取引 |

修正方針: `sourceKind` ごとにGeminiの出力スキーマを切り替えてください。明細用の抽出結果は `[]StatementEntry` 相当とし、各取引の符号・日時・取引番号・原本上の領域を保持します。

### P3 — AIを無効化したときのジョブ検索は、大量の待機ジョブに弱い

[worker_claim.go:33–75](https://github.com/walnuts1018/horse/blob/345d605958294830c18ad048d677aecd9fc96a33/internal/ingestion/worker_claim.go#L33-L75)

`GEMINI_ENABLED=false` の場合、処理できないジョブを1件ずつ `FOR UPDATE SKIP LOCKED` で取得し、除外リストへ追加しながら次の候補を検索します。

待機ジョブが大量に存在すると、一度のポーリングで多くの行を走査し、大きなトランザクションとSQL条件を生成します。

現時点で性能問題が発生したという証拠はありませんが、処理可能なジョブだけを最初のSQLで絞り込む方が単純です。これは実測を伴う低優先度の改善で十分です。

## 修正と同時に追加したいテスト

特に次のケースは、単体テストだけでなく、PostgreSQLを使った統合テストで結果を確認する価値があります。

| テストケース | 期待結果 |
| ---------------------- | ------------------ |
| 5,000円の購入後に2,000円を部分返品 | 支出・支払元・負担額が適切に減る |
| 日付不明の古いレシートを確定 | 明示的な日付確認が必要 |
| 同じ取引番号を持つCSVの2行 | 別々の明細として識別される |
| CSVの2行目を確定後、ページ再読み込み | 該当行が追加済みのままになる |
| 10ページのカード明細PDF | 各取引が独立した観測情報になる |
| メール本文が短く、領収書PDFを添付 | 添付内容を取り出して解析できる |
| OCRで誤読したPDFレシート | 原本照合未完了であることが明示される |

## 現状で維持してよい設計

調べた範囲では、次の判断はそのまま維持するのがよさそうです。

- PostgreSQLをジョブ状態の正本とし、RabbitMQを通知に使う構成。 障害回復とDBトランザクションの整合性を重視した設計として合理的です。
- 整数円による計算と、購入時点の資金拠出ルールのスナップショット。 過去月の精算結果を後から変えないために重要です。
- ZITADEL/OIDC、PKCE、nonce、セッションCookie、家庭所属確認。 認証・認可の基本経路は堅実に実装されています。
- Goで算術検証し、Geminiは原本の観測・解釈を担当させる分離。 LLMに会計上の正しさまで委ねない方針は適切です。

また、GitHub Actionsの `parallel:` 構文は2026年6月から正式にサポートされているため、問題として扱っていません。

[image](https://www.google.com/s2/favicons?domain=https://github.blog\&sz=32)

GitHub Changelog

CIについては、調査時点で [対象コミットの実行](https://github.com/walnuts1018/horse/actions/runs/38472500766) が進行中でした。直前のキャンセルされた実行をテスト失敗と混同すべきではありません。

## 結論と優先順位

今すぐ直す順番としては、①返品・返金の会計モデル、②購入日の確定、③PDFの検証状態、④CSV明細の参照ID統一を推奨します。

続いて、メール添付と複数取引の明細抽出を実装すれば、今後機能を増やしても会計と証拠の対応が崩れにくくなります。

逆に、現段階でORM、GraphQLライブラリ、ジョブキューなどを大きく置き換える必要性は確認できません。まず金額と証拠の正しさを保証する境界を固めることが、Horseにとって最も効果の高い改善です。

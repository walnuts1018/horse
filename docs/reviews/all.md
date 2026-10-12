# Horse 実装レビュー（2026年10月13日）

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse)

確認対象：`main`、commit `5399fecc15b26c9d24a5aba0c057d8bc0e464d5f`

リポジトリの実装を確認しました。すでに会計台帳、複数決済、負担ルール、精算、AI取り込み、オフライン送信、OIDC認証など、かなり広い範囲まで実装されています。

ただし、現段階で修正すべき重要な問題があります。特に、購入登録と確定処理の接続、AI取り込みの原子性、精算対象の欠落は優先度が高いです。

## レビュー結果

重要な修正あり

最優先

- 修正済み 1件
- 未解決 1件

重要

# 5件

改善推奨

# 3件

これはソースコードの静的レビューです。現在のリポジトリにある統合テストや監査記録も参照していますが、私自身がDB統合テストや実ブラウザ操作を再実行したわけではありません。

## P0 — 最優先で直すべき問題

### 1. mutation間で共有した冪等性キーが一意制約に衝突する（修正済み）

公開環境では`createPurchase`が成功した後、同じ冪等性キーを使った`postPurchase`がPostgreSQLの一意制約違反で失敗していました。

各mutationは`household_id`、`key`、`operation`の組み合わせで既存結果を検索していましたが、`idempotency_keys`の主キーは`(household_id, key)`だけでした。そのため、購入作成時の冪等性行と購入確定時の冪等性行が同じキーを使うと、`postPurchase`の登録がSQLSTATE `23505`で拒否されていました。

主キーを`(household_id, key, operation)`に変更し、mutationごとに同じ冪等性キーを安全に使えるようにしました。PostgreSQL統合テストで`CreatePurchase`と`PostPurchase`に同じキーを渡し、購入が`POSTED`になることを確認します。

### 2. AI取り込みで購入作成と証拠の紐付けが別トランザクションになっている

重複登録・不完全保存のリスク

該当箇所

[imports-page.tsx: confirm](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/web/src/pages/imports/ui/imports-page.tsx#L534-L606)

現状の処理は以下の順序です。

1. GraphQLで`createPurchase`を呼ぶ。
2. 購入がDBに作成される。
3. HTTP APIで`/purchases/{id}/evidence`を呼ぶ。
4. 証拠リンクを登録する。

つまり、購入自体の登録は成功したのに、証拠リンクの保存だけが失敗する可能性があります。

実装側でも、この状態を想定して「購入は登録済みです。証拠の紐付けを再試行してください」と表示しています。

問題は、`confirm`を再実行すると新しい冪等キーを生成することです。

```
idempotencyKey: makeIdempotencyKey()
```

証拠リンクの登録が失敗した場合、元文書の確認済み情報もまだ記録されていないため、再実行によって別の購入が作成される可能性があります。

修正案

原本確認を含めた`confirmImportedPurchase`のような単一Mutationに統合します。

内部では、

- 文書・観測情報が属する家庭を確認
- 観測情報の確定済み状態を確認
- 購入・品目・負担・決済を作成
- `POSTED`に確定
- EvidenceLinkとDocumentConfirmationを作成
- 監査イベントを記録

まで、1つのトランザクションで実行します。

また、`sourceDocumentId + sourceReference`を一意に管理し、同じソースの再確認が重複購入にならないようにします。

既存のソース確認APIを利用する手段でも実現できますが、フロントエンドで複数APIを順番に呼んで原子性を実現しようとしないことが重要です。

## P1 — 重要な修正

### 3. 決済元のプールが未設定だと、精算から購入が消える

該当箇所： [accounting.go: loadFundingExpenses](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/graph/accounting.go#L423-L520)

`loadFundingExpenses`では、決済元の口座がプールに属していない場合に、その購入をスキップしています。

```
if !account.PoolID.Valid {
    continue
}
```

複数決済でも同じように、1つでもプール未設定の口座が含まれれば購入全体を除外します。

一方、`createAccount`はプール未設定の口座を許容しており、`postPurchase`も口座とプールの対応を確定条件として検証していません。

そのため、記録・確定できた購入が精算計算から静かに欠落するという問題があります。

これは不正な計算結果を正常に見せてしまうため、かなり危険です。

修正案： 口座のプールを設定することを必須にするか、プール未設定の決済を「未帰属の資金」として明示的に管理する必要があります。

後者を採用する場合、精算では未帰属の決済を検出して未解決状態にし、無言で除外しないようにします。

### 4. プールの資金提供ルール変更によって、過去の購入の精算額が変わる

該当箇所：

- [accounting.go: 資金提供ルールと精算](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/graph/accounting.go#L197-L330)
- [member_burden.go: 負担スナップショット](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/graph/member_burden.go#L35-L140)

ここはすでに実装されているスナップショットと、精算計算の間に整合性上の問題があります。

購入の`POSTED`時には、資金提供ルールに基づいて`purchase_member_burdens`を保存しています。

ところが精算では`poolFundingRules()`で取得した現在のルールを使用し、購入時点のスナップショットを直接利用していません。

例えば、

- 10月1日：共有プールの資金提供割合がA:B=3:2
- 10月5日：共有プール負担の1,000円の購入を確定
- 10月10日：割合をA:B=1:1に変更
- 10月31日：10月分を締める

という場合、購入時に記録されたメンバー負担と、10月末の精算で使われるメンバー負担が異なる可能性があります。

締め済みの精算結果自体は別途保存されているため、締め済みデータを直接書き換える問題とは異なります。しかし、未締めの過去の購入について、支出分析と精算の数字が一致しなくなるのは問題です。

修正案： 購入時に確定したメンバー負担と、実際の決済・立て替えのスナップショットを精算の入力として使用します。

資金提供ルールの変更には有効開始日を持たせ、過去へ適用する変更を認める場合も明示的な再計算・訂正として処理します。

### 5. 締め済み期間に資金移動を追加できる

該当箇所： [account_movements.go: recordAccountMovement](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/graph/account_movements.go#L18-L148)

`recordFunding`や`recordTransfer`では、対象の会計期間が締め済みかどうかを確認しています。

一方、`recordAccountMovement`では、このチェックがありません。

そのため、過去の締め済み期間を`occurredAt`に指定して、口座間移動やカード引落しに相当する仕訳を追加できます。

口座間の移動は支出そのものではありませんが、締め済み期間の口座残高や資金移動履歴を変えられることになります。

また、`createAccount`の期首残高仕訳についても、過去日時を設定する際の会計期間チェックが見当たりません。

修正案： 会計仕訳を生成するすべての経路で、共通の期間ロック・締め確認を行います。

締め後の修正は既存の`Correction`の考え方に合わせて、元日時を記録しながら現在の開いている期間に調整仕訳を追加します。

### 6. AI解析に過去の購入履歴が実質渡されていない

該当箇所：

- [imports-page.tsx: 再解析リクエスト](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/web/src/pages/imports/ui/imports-page.tsx#L390-L440)
- [documents.go: HistoryHint受付](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/api/documents.go#L107-L170)
- [worker.go: AI解析](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/ingestion/worker.go#L292-L325)

`HistoryHint`という仕組み自体は存在します。

しかし、取り込みUIの再解析処理では`hints: []`を渡しています。アップロードAPIも、リクエストされたHintsをそのままジョブへ保存する方式です。

Worker側では、DBから関連する過去の購入や商品を検索してHintsを生成する処理が見当たりません。

つまり、設計上は履歴補完をサポートしているものの、通常のAI取り込みでは空の履歴を渡して推論している状態です。

これは精度に直結します。

修正案： 履歴検索をフロントではなくバックエンドに実装してください。

ただし、最初から全履歴をLLMへ渡すのではなく、次の流れにします。

1. Geminiで原本から商品名・店舗名・金額などの観測情報を抽出する。
2. PostgreSQLから店舗固有の別名や類似商品を検索する。
3. 曖昧な項目についてのみ、候補と過去の訂正履歴を渡して再推論する。
4. 確定した商品マスタからカテゴリ・負担割合を決定する。

これなら画像からの読み取りを過去の情報で汚染せず、トークン消費も抑えられます。

### 7. AIの確認画面で、検証に失敗した金額も帳簿へ保存できる

該当箇所： [imports-page.tsx: AI確定処理](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/web/src/pages/imports/ui/imports-page.tsx#L534-L574)

現在の確定処理では、次のような金額を使用しています。

```
amountYen:
  selectedCandidate?.amounts[index] ??
  (item.grossAmount || item.printedAmount)
```

この書き方には2つの問題があります。

1つ目は、`grossAmount=0`を正しい値として扱えないことです。

JavaScriptの`||`では0がFalsyになるため、税込処理や割引の結果が0円になった場合、印字額に戻ってしまいます。

ここは少なくとも`??`を使用すべきです。

2つ目は、整合性検証が失敗していても、その状態を確定処理が十分に検査していないことです。

候補が存在する場合の選択チェックはありますが、選択した候補が実際に整合しているか、明細合計と購入総額が一致するかを、この確定処理で保証していません。

レシートの税率や値引きを解析できなかった場合に、間違った金額をそのまま家計簿へ記録してしまう可能性があります。

修正案： 確定用Mutationのサーバー側で、修正後の全品目・支払額・税解釈の整合性を再検証します。

ただし、ユーザーが原本を確認して手動訂正した場合には、元レシートの金額と一致しない理由を保持したうえで、明示的に確定できるようにします。

AIの信頼度だけで自動的に登録を拒否する必要はありません。

### 8. 銀行明細が購入の照合対象から一律に除外されている

該当箇所： [reconcile.go: Reconcile](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/ingestion/reconcile.go#L29-L87)

現状の照合処理は、銀行明細を含む場合に以下の条件で除外しています。

```
if incoming.SourceKind == "bank" ||
    old.SourceKind == "bank" {
    continue
}
```

コメントでは、銀行引落しは購入そのものではなく、カード決済債務の履行であると説明されています。

これはクレジットカード代金の銀行引落しについては正しいです。

しかし、銀行明細には次のような取引もあります。

- デビットカードによる商品の直接購入
- 銀行振込による商品購入
- 口座振替によるサブスクリプション料金
- カード利用代金の引落し

これらを全部同じ扱いにしてはいけません。

例えば、銀行明細にデビットカードによるスーパーでの購入が記録されていても、現在の処理ではレシートとの照合候補になりません。

修正案： `sourceKind=bank`だけで判断せず、取引の意味を分類する必要があります。

`PURCHASE_PAYMENT`、`CARD_BILL_PAYMENT`、`ACCOUNT_TRANSFER`、`REFUND`などに分類し、購入に相当する取引だけをレシートや注文履歴と照合します。

分類不能な場合は、勝手に除外・統合せず未解決の観測情報として保持します。

## P2 — 今のうちに改善したい点

### 9. 冪等キーの再利用時、異なる内容でも前回の結果が返る

該当箇所： [mutations.go: createPurchaseの冪等処理](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/graph/mutations.go#L711-L750)

`createPurchase`では、同じ冪等キーが存在する場合、リクエスト内容を検査せず既存の購入を返します。

例えば、同じキーで最初は500円、次に600円の購入を送信すると、後者も正常終了として500円の購入を返す可能性があります。

現状のOutboxではキーを生成して固定しているため、通常の再送では問題になりにくいですが、異なる内容で同じキーを利用することを検出できないのは改善すべきです。

`correctPurchase`にはすでに`RequestFingerprint`を比較する仕組みがあります。

修正案： この方式を共通化し、購入・入金・送金・口座移動など、冪等性を要求するすべてのMutationに適用します。

### 10. 商品サジェストが全商品マスタの取得とN+1クエリになっている

該当箇所： [products.go: productSuggestions](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/graph/products.go#L293-L392)

現状は、家庭の全ProductVariantを取得し、Go側で各商品を走査しています。

さらに、ループ内で`loadProductVariant`と`loadProductConcept`を呼んでいます。

これらは関連する別名・カテゴリなどもDBから取得するため、商品数が増えるとDBクエリ数と処理時間が増加します。

サジェストはユーザーが文字を入力するたびに呼ばれるため、特に改善効果が大きい部分です。

修正案： 検索・スコアリング・上位件数制限を可能な限りPostgreSQL側で実施します。

商品名・正規化名・店舗別Aliasに適切なインデックスを設定し、まずDBで候補を絞り、少数の候補だけをGo側で補正・ランキングします。

今回の検索規模であれば、これだけのためにQdrantやElasticSearchを追加する必要はありません。

### 11. カテゴリの最上位名に一意制約が効かない

該当箇所： [core.sql: categories](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/migrations/20261012000000_core.sql#L35-L47)

現在のカテゴリテーブルには以下の制約があります。

```
UNIQUE (household_id, parent_id, name)
```

しかし、PostgreSQLの通常の`UNIQUE`制約では`NULL`同士を異なる値として扱います。

そのため、`parent_id IS NULL`の最上位カテゴリでは、同じ家庭に「食料品」を複数作成できてしまいます。

修正案：

```
UNIQUE NULLS NOT DISTINCT (
    household_id,
    parent_id,
    name
)
```

PostgreSQL 15以降なら使用できます。現在のPostgreSQL 18を前提とする構成にも適しています。

### 12. AIの有料フォールバックがユーザー操作中でもFlexになる

該当箇所：

- [worker/main.go: Background設定](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/cmd/worker/main.go#L90-L115)
- [router.go: Service Tierの決定](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/internal/ingestion/router.go#L72-L113)

Workerで`Background: true`を設定しているため、通常のレシート撮影であっても無料枠が使えず有料枠へ切り替わる際、Flexが選択されます。

FlexはStandardより安いものの、可変遅延で1〜15分程度の処理を想定したサービスです。

[image](https://www.google.com/s2/favicons?domain=https://ai.google.dev\&sz=32)

ai.google.dev

手入力の簡単さと同様、撮影後にすぐ結果を確認できることも大切です。

修正案： Workerがバックグラウンドで実行されるかどうかではなく、ジョブの目的でTierを選択してください。

ユーザーが確認画面を開いて待っている場合は無料Standard→有料Standard、メールの定期取り込みは無料Standard→有料Flex、大量再解析はBatchという方式がよいです。

## 入力UIについても1点

改善推奨：ユーザー体験

[purchase-create-page.tsx: カテゴリ自動設定](https://github.com/walnuts1018/horse/blob/5399fecc15b26c9d24a5aba0c057d8bc0e464d5f/web/src/pages/purchase-create/ui/purchase-create-page.tsx#L244-L275)

品目入力では、カテゴリが現在のフォーム状態から次の品目へ引き継がれ、過去履歴と商品名が完全一致した場合だけ上書きされます。

例えば衣服を入力した直後に、履歴にない食料品を入力すると、衣服カテゴリが残ることがあります。

これは技術的なバグというよりUX上の判断ですが、当初の「商品名と金額を入力するだけで自然に分類される」という目標から考えると、改善したほうがよいでしょう。

カテゴリの選択状態を「前の品目から継承」「商品マスタから推定」「ユーザーが明示指定」に分け、UIで邪魔にならない形で由来を扱うのがおすすめです。特に手動選択したカテゴリが非同期の履歴補完によって勝手に変更されないようにする必要があります。

## 今の実装で維持したい点

問題だけでなく、現状の良い設計判断もあります。

まず、PostgreSQL中心の会計台帳は維持する方針でよいと思います。購入、資金移動、負担、仕訳、原本の責務が分離されており、整数円と端数配賦の設計も妥当です。

負担ルールのバージョン管理とスナップショットの仕組みも存在しています。全面的に作り直すのではなく、今回指摘した精算計算との接続を修正するのがよいです。

AIについても、Geminiへの直接画像入力、独立したPP-OCRv6、曖昧な税区分の保持、解釈候補の検証、原本領域の保持といった基本構造は、ここまで相談してきた方針に沿っています。

また、`docs/completion-audit.md`に要件ごとの検証状況が書かれている点も評価できます。まだ実施していない検証と完了済みの検証を区別できるため、現時点では未検証であること自体をすべて問題扱いする必要はありません。

## 修正の優先順位

私なら、次の順番で修正します。

| 順位 | 対応 | 理由 |
| -- | ------------------------- | ------------- |
| 1 | 購入登録からPOSTED確定までを一体化 | 支出・精算の正確性 |
| 2 | AI購入確定とEvidenceLink登録の原子化 | 重複購入・不完全登録の防止 |
| 3 | 未帰属の決済を精算から黙って除外しない | 精算漏れの防止 |
| 4 | 購入時点の負担スナップショットを精算に使用 | 過去の負担の整合性 |
| 5 | 全会計Mutationの締め処理を統一 | 確定済み期間の保護 |
| 6 | AIの履歴検索・補完の接続 | 認識精度と入力負担の改善 |
| 7 | AI確認時の金額整合性検証 | 誤入力の防止 |
| 8 | 銀行明細の取引種別を分離 | 二重計上・照合漏れの防止 |
| 9 | 冪等性・DB制約・サジェスト性能 | 品質・堅牢性・操作応答性 |
| 10 | AIのStandard/Flexルーティング | レシート確認の待ち時間短縮 |

## 最終評価

現状のHorseは、根本的に技術スタックを変更する必要はないと考えます。

TanStack Start、React、Relay、Go、gqlgen、PostgreSQL、SeaweedFS、RabbitMQ、Geminiという構成は引き続き適切です。

ただし、購入・精算・AI確定の3つが「正常に保存できたように見えるが、別の台帳・証拠情報と一致していない」状態を許容していることは、今の段階で直すべきです。

この問題は、機能を増やしてから修正しようとすると影響範囲が広がります。特に会計と証拠のトランザクション境界は、現在のうちに整理したいです。

今回のレビューは読み取れたソースに対する静的な確認であり、網羅的な実行検証ではありません。したがって、記載した不整合に対する回帰テストを追加し、`POSTED`後の精算・分析、締め後の変更拒否、AI再試行時の冪等性を実際のPostgreSQL統合テストで確認するところまでを修正完了条件にするのがよいでしょう。

最優先の2件を含め、構造的な問題を今修正すれば、既存の実装を活かしながら完成形へ進められます。

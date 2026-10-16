# Horse リポジトリのコードレビュー

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse?utm_source=chatgpt.com) 確認リビジョン：[`3df9367`](https://github.com/walnuts1018/horse/tree/3df9367a2e13abe27978e4111ec50dda04119340?utm_source=chatgpt.com)（2026年10月17日）

現行の `main` をGitHub連携から確認しました。

まず、実装は当初の構想から大きく進んでいます。バックエンドはGoからRustへ移行しており、Domain / Application / PostgreSQL Adapterを分離した構成です。GraphQLはasync-graphql、DBアクセスはDiesel、フロントエンドはTanStack Start + React + Relayになっています。

レシートの直接画像解析、独立したPP-OCRv6、無料・有料Geminiのルーティング、税額検証、複数ソース照合、共同編集、オフライン下書きまで実装されています。

既存の `docs/reviews`、`docs/decisions/review-responses.md`、`docs/completion-audit.md` も確認し、過去の指摘を可能な限り除外しています。

以下では、コード上で確認した問題と、設計・品質上の改善提案を区別します。なお、今回は静的コードレビューであり、アプリを起動した操作試験や統合テストの再実行はしていません。

## 1. 優先度の高い指摘

現在のレビュー結果

静的解析

| P1 | AI入力トークン予約の過大推定     |
| -- | ------------------- |
| P1 | 5,000件以降の証拠照合候補の欠落  |
| P2 | 過去の購入履歴の活用不足        |
| P2 | 無料枠制限時のUXとフォールバック方針 |

### P1-1. 画像ファイルサイズからGeminiの入力トークン数を推定している

対象：[`adapters/worker-runtime/src/processing.rs:L2236-L2249`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/adapters/worker-runtime/src/processing.rs?utm_source=chatgpt.com#L2236-L2249)

現在の実装です。

```
let document_estimate =
    i64::try_from((document_bytes / 4).max(256))
        .unwrap_or(i64::MAX);
```

これは問題があります。

画像の入力トークン数は、圧縮後のJPEGやPNGのバイト数に比例しません。Geminiが画像をどのように処理するかによって変わります。

例えば、2MiBのJPEGレシートを渡すと、この実装は画像だけで約524,288トークンを予約しようとします。

実際の消費トークンがそれよりはるかに少なくても、`GEMINI_QUOTA_LIMITS`に設定されたTPMがこの値より小さければ、[`gemini_quota.rs:L180-L188`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/contexts/evidence/postgres/src/gemini_quota.rs?utm_source=chatgpt.com#L180-L188) で `InputTooLarge` になり、APIに送信されません。

つまり、本来解析できるレシートが、内部の誤ったトークン推定によって拒否される可能性があります。

また、JPEGの品質設定やPNGへの変換だけで、同じ内容のレシートの推定トークン数が大きく変わります。

改善案

画像についてはファイルサイズではなく、モデルの画像入力方式、解像度、ページ数に基づいて見積もります。

可能ならGeminiの `countTokens` を利用して実際の入力トークン数を取得し、結果を入力のハッシュ・モデル・プロンプトバージョン単位でキャッシュします。

事前見積もりは保守的でも構いませんが、過大推定を理由に永続的な失敗へ分類するべきではありません。画像の実トークン数が不確実な場合は、TPM予約方式と送信可否を分ける必要があります。

これは無料枠の有効利用にも直結するため、優先して修正したい箇所です。

### P1-2. 証拠照合が先頭5,000件で打ち切られる

対象：[`adapters/http/src/lib.rs:L3258-L3311`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/adapters/http/src/lib.rs?utm_source=chatgpt.com#L3258-L3311)

異なるレシート・カード明細・銀行明細を照合する処理では、既存のObservationを次のように取得しています。

```
let existing_rows = source_observations::table
    .filter(source_observations::household_id.eq(household_id))
    .order_by(source_observations::source_document_id.asc())
    .then_order_by(source_observations::source_reference.asc())
    .limit(5001)
```

その後、先頭5,000件だけをメモリ上で照合します。

ここでは、取引金額、注文番号、日時、店舗などによる候補の絞り込みを行っていません。

したがって、家庭のObservationが5,000件を超えると、対象になるべき取引が先頭5,000件に含まれない可能性があります。

`truncated: true` は返されますが、照合対象から除外されたObservationを探し直す処理はありません。

この構造には、主に2つの問題があります。

- 登録履歴が増えるほど、関連する取引を発見できなくなる。
- 全件に近いデータを取得してRustで照合するため、DBからAPIへの転送量とCPU使用量が増える。

改善案

PostgreSQL側で候補を絞り込みます。

まず、正確な注文番号、外部取引ID、原本ハッシュなどで照合候補を抽出します。

次に、日時・金額・店舗・カード識別子などを使って類似候補を検索します。

その後に、Rust側で詳細な照合スコアと矛盾判定を行います。

候補数の上限は最終的なランキングの後に適用し、ソース全体の先頭5,000件という制限は撤廃するべきです。

これは今回のアプリの重要要件である、\*\*「複数ソースから信頼できる取引履歴を構築する」\*\*という機能に直接影響します。

### P2-1. AIによる過去の購入履歴の活用が不十分

対象：

- [`contexts/cross-context/postgres/src/purchase_history.rs:L32-L103`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/contexts/cross-context/postgres/src/purchase_history.rs?utm_source=chatgpt.com#L32-L103)
- [`adapters/worker-runtime/src/processing.rs:L370-L392`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/adapters/worker-runtime/src/processing.rs?utm_source=chatgpt.com#L370-L392)
- [`adapters/worker-runtime/src/processing.rs:L1410-L1459`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/adapters/worker-runtime/src/processing.rs?utm_source=chatgpt.com#L1410-L1459)

履歴を利用する機能自体はありますが、実装は次の制約を持っています。

まず、取得対象が直近20件の購入に限定されています。

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

これは今回の要件の中でも優先したい改善です。

### P2-2. 無料Geminiが制限されたとき、対話的な解析が保留される

対象：

- [`processing.rs:L568-L585`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/adapters/worker-runtime/src/processing.rs?utm_source=chatgpt.com#L568-L585)
- [`processing.rs:L1678-L1697`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/adapters/worker-runtime/src/processing.rs?utm_source=chatgpt.com#L1678-L1697)

これはバグではなく、`docs/decisions/review-responses.md` に記録された意図的な設計判断です。

無料枠で429が返ったときに有料APIを呼ばず、`Retry-After`に従って再試行する方式になっています。

バックグラウンド取り込みなら妥当ですが、スマホでレシートを撮影して結果を確認したい利用者にとっては待ち時間が問題になります。

改善案

処理の緊急度をJobの属性にします。

`INTERACTIVE`なら、無料枠が一時的に利用できない場合でも予算内で有料Standardへ切り替えます。

`BACKGROUND`なら無料枠の回復を待ち、期限が近い場合だけ有料Flexへ切り替えます。

これなら無料枠を最大限利用しながら、レシート入力の体験を損ないません。

また、[`adapters/ai/src/lib.rs`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/adapters/ai/src/lib.rs?utm_source=chatgpt.com) の現在のInteractions APIリクエストには、Flexなどのサービス階層の選択がありません。

Batch APIが未実装であることは既に `docs/reviews/gemini.md` に記録されているので新規の指摘には含めませんが、Flexの対応も料金最適化の観点から検討する価値があります。

### P2-3. AIの出力を集計するときに整数オーバーフローが起こり得る

対象：[`processing.rs:L2591-L2603`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/adapters/worker-runtime/src/processing.rs?utm_source=chatgpt.com#L2591-L2603)

決済額の検証処理です。

```
let sum = payments
    .iter()
    .filter_map(|payment| payment.get("amount").and_then(Value::as_i64))
    .sum::<i64>();
```

LLMが返した値に対して、合計前の範囲検証とオーバーフローチェックがありません。

極端に大きな数値が複数返された場合、Debugビルドではpanicし、通常のReleaseビルドでは整数の折り返しによって不正な合計値になる可能性があります。

また、`filter_map` により数値ではない決済額は無視されるため、不完全な出力を明示的に拒否できません。

改善案

`try_fold` と `checked_add` を使用し、すべての決済行に有効な整数金額が存在することを検証します。

合計が一致するかだけでなく、決済明細の各値が許容範囲に収まることも確認します。

LLMの出力は外部入力なので、ドメイン層に渡す前にすべて検証するべきです。

## 2. バックエンドの設計・保守性

Rust移行後のアーキテクチャは、以前の構想よりも明確に整理されています。

特に、次の点は維持する価値があります。

| 設計 | 評価 |
| -------------------------- | -------------- |
| Bounded Contextごとにcrateを分離 | 適切 |
| Domainの純粋関数と型付き値 | 適切 |
| Workflowと永続化の分離 | 方向性はよい |
| PostgreSQLのトランザクション管理 | 必要な整合性を意識できている |
| 原本と抽出結果の分離 | 適切 |
| 会計の統合テスト | 重要なケースが実装されている |

例えば、個人カードによる共用費の立て替えや、共用口座からの個人支出、部分返金、締め後の訂正に関する統合テストが存在します。

ただし、実行済みの証拠とテストコードの存在は区別する必要があります。

### 巨大なAdapterの責務をさらに分割したい

特に大きいのは次のファイルです。

| ファイル | 規模 |
| -------------------------------------------- | ------ |
| `adapters/http/src/lib.rs` | 約178KB |
| `adapters/worker-runtime/src/processing.rs` | 約127KB |
| `adapters/graphql/src/lib.rs` | 約52KB |
| `adapters/graphql/src/purchase_mutations.rs` | 約44KB |

`adapters/http/src/lib.rs` は、OIDC、GraphQL、文書登録、原本取得、証拠照合、AIとの会話、イベント配信など非常に多くの責務を持っています。

同様に、`processing.rs` にはモデルルーティング、quota、プロンプト、OCR、税検証、履歴検索、費用計算、SSE解析が集まっています。

これは機能上の誤りとは限りませんが、変更範囲が大きくなり、個別機能のテストや理解が難しくなります。

追加crateを大量に増やす必要はありません。 まずは既存Adapterの中で、HTTPハンドラ、認証、原本アクセス、照合、AIクライアント、抽出処理、検証処理などのモジュールを明確に分けることを勧めます。

Domain Modeling Made Functionalへの準拠性そのものの問題は、すでに `docs/reviews/dmmf.md` に詳細なレビューが存在するため、ここでは繰り返しません。

## 3. フロントエンドと入力UX

フロントエンドについては、構造上は改善が進んでいます。

購入入力、帳簿、取り込み、分析、精算がページ単位に整理され、Reactの責務もある程度分離されています。

また、以下の機能が存在することを確認しました。

- 購入単位での共通項目管理と複数品目の入力
- 商品の候補表示
- 複数決済元への配分
- IndexedDBによる下書き保存とOutbox
- 購入の共有下書き
- レシートの品目別レビュー
- 証拠画像の領域指定と訂正
- 税額候補の選択と手動金額修正
- 帳簿の検索・ページング

入力UIをゼロから作り直す必要がある状態ではありません。

一方、最重要要件である「手入力の操作回数が少ないこと」を評価するには、コンポーネント構造だけでは不十分です。

特に次の操作は、実ブラウザで確認したいです。

| 操作 | 評価すること |
| ------------- | --------------------- |
| スマホで5品目を連続入力 | タップ数、キーボードの切り替え、フォーカス |
| 入力済み品目の金額修正 | 編集開始から保存までの操作数 |
| 4品目の負担割合を一括変更 | 選択、変更、解除までの操作 |
| AIで誤認識した品目の修正 | 原本の確認と訂正の往復回数 |
| レシートの税額矛盾 | 問題箇所を探す時間 |
| 入力途中の画面離脱・復帰 | 下書きの復元と競合表示 |
| ほかのユーザーとの同時編集 | 不意のフォーカス移動や入力消失 |

これらはPlaywrightで操作シナリオを定義し、入力所要時間や操作回数を測る方式が適しています。

現行のUIが悪いと断定する根拠はありませんが、これらを実測しない限り、手入力が最適化されているとも判断できません。

## 4. テスト・リリース品質

CIにはRust、Web、OCRのテストとBrowser E2Eが組み込まれています。

ただし、[`ci.yaml:L157-L163`](https://github.com/walnuts1018/horse/blob/3df9367a2e13abe27978e4111ec50dda04119340/.github/workflows/ci.yaml?utm_source=chatgpt.com#L157-L163) の `publish` は `quality` に依存していますが、`browser-e2e` には依存していません。

したがって、Browser E2Eが失敗しても、Qualityさえ成功すればイメージ公開が進み得ます。

これは `docs/completion-audit.md` にも既知の状態として記録されています。

最初から完成形の品質を重視する方針なら、E2Eをリリース条件に含めることを推奨します。特に認証、購入登録、精算、レシート確認といった主要操作は重要です。

一方、すべてのテストを無差別に増やすのではなく、実際の会計不変条件と利用者の操作シナリオをカバーすることを優先するべきです。

## 5. 修正の優先順位

今回新たに確認した内容に限定すると、私なら次の順番で改善します。

| 優先 | 改善 | 理由 |
| -- | --------------------- | ------------------ |
| 1 | Gemini画像のトークン予約方式を修正 | 正常な原本の解析を拒否し得る |
| 2 | Observation照合のDB側絞り込み | 履歴増加で候補が欠落する |
| 3 | AI出力金額の厳密な検証 | 不正値・オーバーフロー防止 |
| 4 | 過去履歴の検索・活用方式を改善 | 商品正規化とAI品質に直結 |
| 5 | 対話型Jobの有料フォールバック | レシート入力の待ち時間を短縮 |
| 6 | 巨大Adapterの責務分割 | 実装の理解・テスト・変更を容易にする |

## 総評

現状のHorseは、当初の構想に対してかなりの機能が実装されています。

特に、会計の不変条件、精算、訂正、複数ソースの照合、Rustのドメインモデリングは、単純なCRUDアプリ以上の設計になっています。

今回、重点的に読んだ範囲で気になったのは、AIの前後処理と履歴の活用、データ量が増加した場合の照合処理です。

これらは新しい機能を増やすよりも先に、既存の能力を正しく発揮させるために改善する価値があります。

なお、今回は全1,207エントリのリポジトリ構造を把握し、AI Worker、主要なHTTP・会計関連コード、フロントエンドの購入・取り込みフロー、設計文書、テスト構成を中心に確認しました。全ソースを行単位で網羅したレビューではなく、実行環境での動作検証も含みません。

そのため、上記の問題は確認範囲における具体的な指摘であり、これ以外に問題がないことを保証するものではありません。

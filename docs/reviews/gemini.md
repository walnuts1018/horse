## 結論：無料枠は優先されていますが、まだフル活用できていません

最新の [`walnuts1018/horse`](https://github.com/walnuts1018/horse) の `main`（`b5085e2`）を確認しました。

現在の実装は「まず無料APIを試し、使えなければ有料APIへ切り替える」方式です。 これは良い出発点ですが、「有料APIを使う前に、利用可能な無料枠を最大限使い切る」方式にはなっていません。

このレビュー後、無料枠で`429`が返った場合は有料へ切り替えず、APIの`Retry-After`をWorkerのjob再試行へ渡すよう変更しました。現在も無料枠のモデル別使用量や日次制限を共有管理する機能はありません。

### 現在の評価

| 項目 | 評価 |
| ----------------------- | ------------- |
| Free Projectを優先する | &#x20;実装済み |
| レシートとメールでモデルを使い分ける | &#x20;実装済み |
| 無料枠の429時に待機して無料APIを再試行 | 実装済み。Retry-AfterをWorker再試行へ引き継ぐ |
| 無料枠の一時的な5xx時に有料へフォールバック | 実装済み |
| バックグラウンド処理で有料Flexを使う | &#x20;経路は実装済み |
| モデル別の無料RPM・TPM・RPDを把握する | &#x20;未実装 |
| 無料枠の回復を待って処理する | &#x20;未実装 |
| 別の無料モデルの余力を利用する | &#x20;未実装 |
| 再解析・Pro昇格の費用を最適化する | &#x20;改善余地あり |
| 有料APIの月間予算を制御する | Google Cloud Spend Capで月500円を設定。アプリ内の予算予約は追加しない |

Google公式によると、無料枠のレート制限はプロジェクト単位で、主にRPM（分間リクエスト）、TPM（分間入力トークン）、RPD（日間リクエスト）で管理されます。実際の上限はプロジェクト・モデル・利用状況によって異なります。

[image](https://www.google.com/s2/favicons?domain=https://ai.google.dev\&sz=32)

ai.google.dev

### 重要：現在の公開環境では、Geminiが無効になっている記録があります

[docs/completion-audit.md](https://github.com/walnuts1018/horse/blob/b5085e2de82390e783c6fe0c37da2a702c0cf54d/docs/completion-audit.md) には、Free/Paid両プロジェクトで合成画像を使った実API呼び出しが各1回成功した一方、検証後に通常Workerの `GEMINI_ENABLED` を `false` に戻した、と記録されています。

したがって、この記録が現在のデプロイ状態と一致していれば、通常の画像・PDF取り込みでは無料APIも有料APIも使用されていません。CSVのローカル解析は引き続き可能です。

これは料金を節約するためのルーティングとは別の話です。コード上で無料枠優先が実装されていても、Workerを有効化していなければ利用実績は増えません。公開環境の現設定は別途確認が必要です。

## 料金面で直すべき具体的な箇所

### 1. 現行APIからは有料Flexをほとんど活用できない

[documents.go](https://github.com/walnuts1018/horse/blob/b5085e2de82390e783c6fe0c37da2a702c0cf54d/internal/api/documents.go) では、新規アップロードと手動再解析のジョブに `JobPurposeInteractive` を設定しています。

`JobPurposeBackground` を渡した場合にFlexへ切り替える処理はありますが、現在のAPIから作るジョブにはその経路がありません。

Google公式のFlexは、通常の有料料金から 50%割引になる代わりに待機時間と可用性の制約があります。Batch APIも大量の非同期処理向けに50%割引で提供されています。

[image](https://www.google.com/s2/favicons?domain=https://ai.google.dev\&sz=32)

ai.google.dev

+1

そのため、メールの一括取り込みや過去履歴のインポートには、別のジョブ優先度と処理期限を与える仕組みが必要です。ただし、無料で処理できるなら無料Standardを使う方が、半額の有料Flexよりさらに安いことは維持してください。

### 2. 無料モデル間のquota fallbackが限定的

現在のモデル選択は、基本的に次の構成です。

| 処理 | 初回モデル |
| ------------- | ---------------------------- |
| レシート・画像・PDFなど | `gemini-3.8-flash` |
| メール | `gemini-3.5-flash-lite` |
| 高度な品質再検証 | `gemini-3.1-pro-preview`（有料） |

メール・注文メールでは、無料Liteモデルの429時に無料Flashモデルを試す経路を追加しました。無料Flashも429ならRetry-AfterをWorkerへ返し、一時的な5xxなら既存方針どおり有料Flashへfallbackします。レシートのFlashからLiteへ品質を落としてfallbackする経路や、モデル別の無料枠の共有状態管理はありません。

このfallbackは429を受けたメールに限定し、無料枠が残っているモデルへの切替を試します。使用量、モデル別RPM・TPM・RPDを追跡する処理や、全source kindを横断する汎用fallbackではありません。

Google公式の料金表では、現在のモデルに加えて `gemini-3.1-flash-lite` も無料Standardに対応しています。

[image](https://www.google.com/s2/favicons?domain=https://ai.google.dev\&sz=32)

ai.google.dev

+2

ただし、レシートをすべて最安モデルにすることは推奨しません。税率混在・値引・ポイント・商品名の判読精度が落ちれば、再解析費用と人手修正が増えるからです。

なお、`gemini-3.1-flash-lite` は有料でも比較的安価ですが、現在の [modelPrices()](https://github.com/walnuts1018/horse/blob/b5085e2de82390e783c6fe0c37da2a702c0cf54d/internal/ingestion/router.go#L330-L347) に登録されていません。そのまま `GEMINI_LITE_MODEL` に指定すると、無料呼び出し後の有料フォールバックが `unknown_model_price` で拒否されます。候補モデルを増やす際は料金表も変更する必要があります。

### 3. 有料APIの月間利用額を呼び出し前に制御できない

[gemini.go:81–117](https://github.com/walnuts1018/horse/blob/b5085e2de82390e783c6fe0c37da2a702c0cf54d/internal/ingestion/gemini.go#L81-L117)

成功・失敗にかかわらず、Gemini API応答にUsageMetadataがあればモデル、入力・出力トークン数、為替レート、推定費用を記録します。月間の予算予約や呼び出し前の上限判定はRouterにありません。Google側のSpend Capを利用しているため、アプリ内に同じ月間上限を重ねず、Google請求データとの照合を改善する場合に再評価します。

## 私なら、このようなルーティングへ変更します

取り込みジョブを受信

ファイル種類、品質要件、処理期限を判定

対話的な取り込み

待機時間を重視

無料Flash / Lite

短時間の無料リトライ → 別の無料モデル → 有料Standard

バックグラウンド

無料消費を優先

無料Flash / Lite

無料枠の回復待ち → 別の無料モデル → 期限に応じて有料Flex / Batch

ローカルの金額検証・OCR照合

問題が残る場合だけ部分再解析。重大で解決可能な問題はProへ昇格

この構成にする場合は、無料プロジェクトについてモデルごとのRPM・TPM・RPD使用状況を共有状態で管理する必要があります。

Horseは既にPostgreSQLとValkeyを利用する構成なので、Workerを複数起動しても同じ無料枠を過剰消費しないよう、共有のレートリミッターを設けるのが適切です。上限値はAI Studioの実際の値から設定し、`429` を受けたら利用可能時刻を更新します。

## 優先順位

| 優先度 | 改善項目 | 期待する効果 |
| --- | ------------------------- | ---------------- |
| P1 | 別の無料モデルへのフォールバック | 残った無料処理枠を活用 |
| P1 | 有料Pro昇格条件とOCR再解析の費用対効果を評価 | 人手修正を減らす再解析に有料利用を限定 |
| P2 | Batch・Flexの実用的なジョブ分類 | 無料で収まらない大量処理を半額化 |
| P2 | モデル別の実レシート精度・費用ベンチマーク | 最も安く正確なモデルを選択 |

なお、Horseには既に [`internal/evaluation`](https://github.com/walnuts1018/horse/tree/b5085e2de82390e783c6fe0c37da2a702c0cf54d/internal/evaluation) のオフライン評価器があります。新しい評価基盤を作り直すのではなく、ここへ日本語レシートとメールの正解データを増やし、1件の正しい購入登録にかかった総費用でモデル選択を比較するのがよいです。

最も効果が大きいのは、モデルをただ安いものに置き換えることではありません。無料枠が一時的に使えないだけなのか、本当に使い切ったのかを判断し、急がない処理を無料枠の回復まで待たせることです。

現在は「Free First」まではできていますが、目指すべき「Free Maximization」にはまだ改善の余地があります。

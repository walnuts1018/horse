# Gemini連携の未解決項目

## 本番のAPI利用条件

2026-10-15に[Gemini API Additional Terms of Service](https://ai.google.dev/gemini-api/terms?hl=en)を確認しました。同規約は2026-03-23発効です。Unpaid Servicesでは入力・応答が製品改善・開発に使われ、有人レビューされる場合があるため、個人・機密情報を送らないよう定めています。同規約はGemini APIをprofessional or business purposes向けとし、consumer useを認めていません。領収書、注文メール、カード明細などの本番文書はGemini Developer APIのFree・Paid projectへ送信しません。本番WorkerはDeveloper API処理を停止し、ローカルOCRと手入力を維持します。offline fixture evaluatorはAPIを呼び出しません。Vertex AIを含む代替APIの利用条件、データ保持、料金、モデル提供状況、認証方式の確認が未完了です。確認が済むまでDeveloper APIを本番で再開しません。

## 1. Batch API

Gemini Batch APIのクライアント、job状態管理、結果取得経路は未実装です。大量のbackground jobで通常APIのquota予約がボトルネックになる場合に、Batch API対応を検討してください。

## 2. source kindごとの精度評価

オフライン評価器はsource kindごとに、品目検出precision・recall、印字金額一致率、税額整合率、名称正規化一致率、証跡precisionを集計し、保存済みの抽出実行がある場合はモデル別の精度、推定費用、平均レイテンシも集計します。カード明細は取引行を品目評価へ変換し、加盟店名・金額・証跡領域を比較します。カード明細には税額整合率を適用せず、`taxConsistencyDocuments`を0にします。`mise run evaluation:run`で出力JSONの`sourceKinds`を確認できます。Gemini APIは呼び出しません。

同梱fixtureは架空のレシート、カード明細、注文メールを含み、税区分付き品目、値引、ポイント利用、明細行、証跡領域を評価器へ通します。fixtureにはモデル名・費用・遅延・モデル出力を記録していないため、出力されたsource kind別の精度は評価器の動作例であり、AIモデルの精度や実利用時の費用を示しません。実モデルを比較するには、同意を得た非公開評価データ上で各モデルの保存済み出力を評価データセットへ追加する必要があります。source kindごとの実測精度、モデル比較、費用、レイテンシは未評価です。既定モデルや自動振り分けを変更する根拠もありません。

正解ラベルのない試行記録があります。この記録をモデル精度の根拠として扱いません。

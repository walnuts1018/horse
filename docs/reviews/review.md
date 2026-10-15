# Horse コードベース追加レビュー

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse) 確認したコミット：[`70338cc`](https://github.com/walnuts1018/horse/commit/70338cc5822115b2f9a39b7934fe866b0a717a48)

`docs/reviews` の4ファイルを確認し、そこですでに扱われている指摘を除外しました。さらに、`docs/decisions/review-responses.md` の過去の対応記録も参照しています。

今回は動作不良だけではなく、会計データの整合性、非同期処理、同時編集、フロントエンドの状態管理、パフォーマンス、保守性、細かなコード品質まで対象にしました。

以下は実際のコードから確認できた問題です。コードの静的レビューであり、ローカルでのテスト実行や本番環境の再現試験は行っていません。

## 1. 優先して修正したい問題

### P1 — 月途中の入金予定変更で、精算額と保存される入金義務が一致しなくなる

対象コード

- [internal/application/settlement/calculation.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/application/settlement/calculation.go#L261-L330)
- [internal/application/settlement/service.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/application/settlement/service.go#L246-L280)
- [internal/application/funding_schedule/service.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/application/funding_schedule/service.go#L101-L153)

同じメンバー・同じプールに対する入金予定を月途中に変更した場合の扱いが不整合です。

例えば、10月1日時点の月額入金予定が4,000円で、10月15日から6,000円に変更したとします。

現在の処理では、次のことが起こります。

1. 新しい入金予定を設定すると、以前の予定の終了日が10月14日になる。
2. 精算計算では、10月中に存在した両方の月額スケジュールを合計し、10,000円として扱う。
3. 締め処理の`materializeFundingObligations`では、同じ月・メンバー・プールに対するスケジュールを1件ずつUPSERTする。
4. 最後のUPSERTが前の金額を上書きするため、保存される入金義務は6,000円または4,000円になり得る。

つまり、精算に使う金額と、履歴として保存する入金義務の金額が一致しません。

さらに、月途中の予定変更を「両方満額計上」「日割り」「新しい予定だけ適用」のどれとして扱うかも、現在の実装では明確ではありません。

修正案： まず月途中の変更ポリシーをドメイン仕様として確定させ、対象月・メンバー・プールごとに入金義務を一度だけ算出します。その結果を精算計算と永続化の両方で使い、1キーにつき1回のUPSERTに変更してください。

月途中の変更を複数回行うケース、週次と月次の境界、締め後に予定を変更するケースもテスト対象にすべきです。

### P2 — 一時的なネットワーク障害でも、オフライン送信が自動再試行されない

対象コード

- [use-purchase-outbox-sender.ts](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/app/model/use-purchase-outbox-sender.ts#L65-L129)
- [purchase-storage.ts](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/shared/lib/purchase-storage.ts#L450-L540)

送信時に`TypeError`や`NetworkError`が発生しても、`markPurchaseOutboxFailed`で状態を`FAILED`に変更しています。

一方、送信処理は`PENDING`のエントリだけを対象にしています。

そのため、スマホで一時的に通信が切れただけでも、その送信は接続回復後に自動再開されません。手動の再試行が必要になります。

オフライン対応の家計簿としては、入力完了後に利用者が再送を意識しなくて済むほうが自然です。

修正案： 一時的な通信障害は`PENDING`のまま保持し、`nextRetryAt`と指数バックオフを持たせて自動再送します。サーバーでのバリデーション失敗など、再試行しても成功しないエラーだけを`FAILED`にします。

送信がサーバーで成功した直後に通信が切れるケースについても、既存の冪等キーを利用して安全に再送できることを保証します。

### P2 — 分析画面で購入履歴全件を取得するため、データ量に比例して操作が重くなる

対象コード

- [use-analysis-data.ts](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/analysis/model/use-analysis-data.ts#L40-L65)
- [use-household-purchases.ts](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/entities/purchase/api/use-household-purchases.ts#L303-L310)
- [analysis-page.tsx](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/analysis/ui/analysis-page.tsx#L60-L90)

カテゴリ別・メンバー別はサーバー側集計を利用していますが、店舗・品目・プール・決済元については`loadAll: true`で対象期間の購入履歴を全件取得しています。

取得は100件単位で繰り返され、全件取得が完了するまで分析結果を表示しません。

例えば対象期間に10,000件の購入があれば、おおむね100ページ分の購入データを転送することになります。しかも必要なのは集計値なのに、購入の詳細を丸ごと取得しています。

修正案： 店舗・品目・プール・決済元の集計もPostgreSQL側で実施してください。GraphQLでは集計対象、期間、必要なグループだけを取得するAPIにします。

商品単価の推移など、個々の取引が必要な部分だけを別クエリとして取得する構成に分離すると、分析画面が軽くなります。

### P2 — 特定のメールを境に、メール取り込みが永久に進まなくなる可能性

対象： [internal/ingestion/mailbox/mailbox.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/ingestion/mailbox/mailbox.go#L200-L240)

`MaxMessageBytes`と`MaxBatchBytes`は、それぞれ独立して設定できます。

しかし、メッセージが`MaxMessageBytes`以下であっても、`MaxBatchBytes`より大きい場合、`fetchMessage`は`ErrBatchLimit`を返します。

呼び出し側はこのエラーで処理を中断し、当該UIDを処理済みにしません。

例えば、メールの上限が10MB、バッチ上限が1MBの構成で、2MBのメールが到着した場合です。

次回以降も同じメールがバッチ先頭に来るため、後続メールの取り込みが進まなくなります。

修正案： バッチ先頭のメールがバッチ容量を超える場合は、別の制御を行うべきです。設定段階で`MaxBatchBytes >= MaxMessageBytes`を要求する方法が最も簡単です。

現在のテストは途中でバッチ容量に達するケースを確認していますが、最初の1件が容量を超えるケースも追加してください。

### P2 — メール取り込み中、DBトランザクションを開いたまま外部通信している

対象： [internal/api/mailbox_poller.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/api/mailbox_poller.go#L90-L145)

`poll()`では、次の処理を1つのDBトランザクション内で実行しています。

1. `mailbox_cursors`を`FOR UPDATE`でロックする。
2. 外部IMAPサーバーへ接続してメールを取得する。
3. メールを解析して、文書登録とジョブ投入を行う。
4. 最後にカーソルを更新してコミットする。

ネットワーク通信中もDB接続と行ロックを保持するため、通信が遅い場合やメールが大量にある場合のリソース占有時間が長くなります。

さらに、文書登録は別トランザクションで確定するため、途中で失敗すると、メールの処理結果とカーソルのコミット状態がずれる可能性があります。

修正案： IMAP取得とDBカーソル更新を分離し、短いトランザクションでカーソルを確定してください。複数Workerによる競合は、短期リースやCASで制御できます。文書登録側の冪等性も併せて検証します。

### P2 — 返金照合でも過去の購入履歴を全件取得している

対象： [use-import-confirmation.ts](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/imports/model/use-import-confirmation.ts#L120-L133)

選択した書類に返金明細が含まれていると、`useHouseholdPurchases`へ`loadAll: true`を渡しています。

このため、1件の返金を確認するために、家庭の購入履歴を全件取得する可能性があります。

購入履歴が増えるほど返金確認画面の読み込みが重くなり、ネットワーク通信量とブラウザのメモリ使用量も増加します。

修正案： 返金候補検索をサーバー側に移してください。購入日時・店舗・金額・カード情報などを条件に候補を絞り、必要な購入だけ取得します。

### P2 — ページネーションのカーソルが、購入日時の編集によって移動する

対象：

- [internal/graph/pagination.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/graph/pagination.go#L25-L40)
- [internal/graph/query_purchases.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/graph/query_purchases.go#L177-L215)

購入履歴のページネーションは、購入日時とUUIDによってソートされています。

しかし、カーソルにはUUIDしか含まれていません。

次のページを取得するときに、そのUUIDから現在の購入日時を取得し、ページ境界に利用しています。

そのため、別のユーザーが境界となる購入の日時を編集すると、カーソルが指す位置が移動します。

結果として、購入履歴が重複して表示されたり、一部が飛ばされたりする可能性があります。

修正案： カーソルにソートキーである`occurredAt`と`ID`の両方をエンコードし、カーソル作成時の位置を保持してください。

リアルタイムで更新される一覧で完全なスナップショット整合性まで必要なら、変更カーソルやクエリスナップショットの導入も検討します。

## 2. フロントエンド・入力UX

### P2 — オフライン専用入力画面では自動保存されない

対象： [web/public/offline-shell.js](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/public/offline-shell.js#L125-L260)

通常の購入入力画面では、入力変更を検知してIndexedDBへ自動保存しています。

一方、Service Workerが表示するオフライン専用入力画面では、フォームの送信操作を実行するまでIndexedDBへ変更が保存されません。

`beforeunload`による警告は実装されていますが、スマートフォンではOSによるブラウザの終了やバックグラウンドでのプロセス破棄が発生することがあります。

通常の入力画面とオフライン画面で、保存タイミングが異なるのは好ましくありません。

修正案： オフライン画面でも入力変更後にDebounce付きの自動保存を行い、保存状態を明示してください。

通常画面とオフライン画面が同じIndexedDBスキーマを利用する以上、下書きの永続化ロジックもできるだけ共通化したいところです。

### P3 — レシートのローカル下書きだけ、利用者単位で分離されていない

対象： [web/src/shared/lib/purchase-storage.ts](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/shared/lib/purchase-storage.ts#L72-L105)

通常の購入下書きは`memberId + householdId`をキーにしています。

しかし、AIレシートの確認下書きは、

```
JSON.stringify([householdId, documentId])
```

というキーです。

同じブラウザを複数人で利用すると、別ユーザーが同じ書類を開いた場合に、以前のユーザーがローカルで変更した確認内容を復元する可能性があります。

これは必ずしもセキュリティ問題ではありませんが、未確定の編集内容が利用者間で意図せず引き継がれます。

修正案： ローカルの確認下書きも`memberId`で分離してください。

もし確認状態を家庭内で共有したいなら、ブラウザのローカルストレージを利用するのではなく、サーバー側の共有下書きとして明示的に同期するほうが設計上自然です。

### P3 — 返金を含む分析で、内訳が実際の負担先と一致しない場合がある

対象： [web/src/pages/analysis/model/purchase-analytics.ts](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/analysis/model/purchase-analytics.ts#L35-L115)

現在の`purchaseDimensions()`では、購入の負担プール別内訳に元の配賦額を加算し、訂正金額を「プール未配賦の訂正」という独立した項目へ追加しています。

既存テストでもこの動作が期待されています。

総額は一致しますが、例えばA個人の服を返品した場合、Aのプールの支出額が減るのではなく、「プール未配賦の訂正」がマイナスになります。

つまり、家計全体の支出は正しくても、プール別の実質負担分析には返品が反映されません。

修正案： 訂正・返金の負担先が明確な場合は、対応するプールに訂正額を計上してください。負担先が不明な訂正に限って「未配賦」を使用する設計がよいと思います。

### P3 — マイナスの支出を棒グラフで適切に表示できない

対象： [web/src/pages/analysis/ui/analysis-breakdown.tsx](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/analysis/ui/analysis-breakdown.tsx#L170-L198)

内訳の棒グラフでは、棒の幅を以下の式で求めています。

```
Math.max(8, (row.amount / (rows[0]?.amount || 1)) * 100)
```

マイナスの金額が含まれる場合、正負を適切に区別できません。

月別推移グラフには正負を別表示する処理があるのに、内訳グラフでは対応が統一されていません。

修正案： 正負それぞれの棒を明示的に表示してください。少なくとも、負の値を正の値と同じ最小幅で描画しないようにするべきです。

## 3. AI・データ取り込み

### P3 — 日本語のテキストをUTF-8の途中で切断する可能性

対象： [internal/ingestion/router.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/ingestion/router.go#L639-L655)

`evidenceGuidance()`では、検証エラーやOCR結果をプロンプトに埋め込む際に、以下のような処理を行っています。

```
joined = joined[:6000]
line.Text = line.Text[:400]
```

Goの文字列スライスはバイト単位です。

そのため、切断位置が日本語のマルチバイト文字の途中に当たると、UTF-8として不正な文字列が生成されます。

JSON化の過程で置換文字に変わる可能性があり、ちょうど境界にある品名や金額情報を失います。

修正案： UTF-8の文字境界を保持した切り詰め処理を利用してください。

バイト数の制限を維持したいなら、上限位置から最後の完全なRune境界まで戻す方式が簡単です。

### P3 — 高性能モデルへのエスカレーション条件が、日本語のエラーメッセージに依存している

対象： [internal/ingestion/router.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/ingestion/router.go#L630-L637)

`needsProReview()`では、検証エラーの文章に「税率」「税額」「小計」「合計」などの文字列が含まれるかどうかで、Proモデルを使用する必要性を判断しています。

これは表示用の文章と内部制御が密結合している状態です。

例えば、エラーメッセージを「税込金額が一致しません」から「税込価格の整合性を確認してください」に変更するだけで、モデル選択に影響する可能性があります。

修正案： 検証結果には、文章とは別に機械的に判別できるIssue Codeを持たせてください。

```
type VerificationIssueCode string

const (
    IssueTaxMismatch   VerificationIssueCode = "TAX_MISMATCH"
    IssueTotalMismatch VerificationIssueCode = "TOTAL_MISMATCH"
    IssueMissingItem   VerificationIssueCode = "MISSING_ITEM"
)
```

UI用のメッセージはIssue Codeから生成し、モデル選択はCodeだけで判断する構成がよいです。

### P3 — Geminiのモデル料金がGoコードに直接埋め込まれている

対象： [internal/ingestion/router.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/ingestion/router.go#L672-L697)

`modelPrices()`には、モデルごとの入力・出力単価と料金変更日が直接記述されています。

料金変更日が分かっているものには対応できていますが、Googleの料金表が変更されるたびに、アプリケーションを修正・デプロイする必要があります。

また、未知のモデルでは料金が0として返されます。有料実行時には別の検証もありますが、料金表示や評価系の計算で未知モデルを0円と扱う設計は避けたいです。

修正案： 料金表をモデル・期間・Service Tierごとのバージョン付き設定として管理し、未知の料金は0円ではなく「料金未設定」として扱ってください。

料金設定の更新をアプリ本体のリリースと分離できます。

### P3 — Geminiへの呼び出し前に、毎回トークン計数APIを呼び出す経路がある

対象： [internal/ingestion/gemini_client.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/ingestion/gemini_client.go#L18-L65)

Quota Reservationが有効な場合、`CountTokens`を呼び出し、その後に`GenerateContent`を呼び出しています。

これはリクエスト前に正確な入力トークン数を把握するうえで合理的な処理です。

一方、レシート画像のような比較的大きなデータでは、外部APIとの往復が1回追加されます。

改善案： `CountTokens`にかかる時間を実測し、全体のレイテンシに占める割合を確認してください。

厳密なQuota管理が必要な無料ルートでは維持し、有料ルートなどでは推定トークン数による予約を許容できるか検討する価値があります。

これは不具合ではなく、性能最適化の候補です。

## 4. バックエンドの保守性・コード品質

### P3 — HTTPレスポンスのJSONエンコードエラーを無視している

対象： [internal/api/server.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/api/server.go#L395-L401)

共通の`writeJSON()`は、HTTPステータスを先に送信し、JSONエンコード・書き込みのエラーを無視しています。

```
func writeJSON(w http.ResponseWriter, status int, value any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.MarshalWrite(w, value)
}
```

レスポンスのエンコードに失敗すると、HTTPステータスは成功なのにJSON本文が不完全という状態が発生し得ます。

修正案： レスポンスを送信する前にJSONをエンコードし、失敗した場合は適切なエラーを返す方式にしてください。

ネットワークへの書き込みエラーは応答をやり直せませんが、少なくとも内部ログには記録できます。

### P3 — Originの比較方法がAPIと認証処理で統一されていない

対象：

- [internal/api/server.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/api/server.go#L402-L409)
- [internal/auth/oidc.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/auth/oidc.go#L349-L367)

REST API側は`Origin`ヘッダーを文字列の完全一致で比較しています。

一方、認証側はURLをパースし、SchemeとHostを比較しています。

例えばホスト名の大文字・小文字や設定値の末尾表記が異なる場合、認証処理とREST APIで結果が変わる可能性があります。

修正案： Originの判定を共通ライブラリにまとめ、正規化したScheme・Host・Portで判定してください。

認証とAPIの両方で、同じテストケースを利用できるようにします。

### P3 — 入金予定の周期について、DB・ドメイン・GraphQLで対応範囲が異なる

対象：

- [internal/application/funding_schedule/service.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/application/funding_schedule/service.go#L55-L61)
- [internal/application/settlement/calculation.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/application/settlement/calculation.go#L338-L375)
- [graphql/funding_schedule.graphql](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/graphql/funding_schedule.graphql#L1-L12)

入金予定の計算エンジンは`MONTHLY`、`WEEKLY`、`CUSTOM`に対応しています。

DBも3種類を許容しています。

しかし、Application Serviceで新規設定できるのは`MONTHLY`のみで、GraphQLのenumも`MONTHLY`だけです。

現在の公開APIでは月次だけを提供する方針かもしれませんが、内部の対応範囲と公開契約が分かれています。

改善案： 月次のみが正式仕様なら、週次・任意日付の計算処理を将来用コードとして曖昧に残さず、仕様とテストを整理してください。

逆に、週次・任意日付を提供したいなら、GraphQL、Application、UIまで一貫して対応させるべきです。

## 5. アーキテクチャ・開発体験

### P3 — 一部のファイルに複数の独立した責務が残っている

コードベース全体では、Application ServiceやGraphQL Resolverなどの分離は進んでいます。

しかし、まだ責務が集中しているファイルがあります。

| ファイル | 規模 | 混在している責務 |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | ----------------------------- |
| [internal/api/documents.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/api/documents.go) | 約1,570行 | アップロード、一覧、ページネーション、再解析、証拠確認など |
| [use-import-confirmation.ts](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/imports/model/use-import-confirmation.ts) | 約1,070行 | レシート編集、返金、貸付、送金、入金、照合 |
| [statement-review.tsx](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/imports/ui/statement-review.tsx) | 約1,070行 | 明細確認と各種取引の編集・候補選択 |
| [edit-purchase-items.tsx](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/purchase-create/ui/edit-purchase-items.tsx) | 約1,000行 | 品目編集、候補選択、貼付、一括操作 |
| [ledger-page.tsx](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/web/src/pages/ledger/ui/ledger-page.tsx) | 約885行 | 一覧、検索、選択、編集、証拠表示 |

以前のレビューを受けて画面分割が進んだことは確認できますが、単にJSXを別ファイルに移すだけでは解決しません。

例えば`use-import-confirmation.ts`なら、次のような責務で分けられます。

- レシートの編集状態と訂正履歴
- 購入確定
- カード・銀行明細の確認
- 返金・訂正の関連付け
- 貸付・返済の関連付け

ただし、ファイルを細かくすること自体を目的にはしないでください。

関連する状態と操作を1つのモジュールに閉じ込め、外部には分かりやすいインターフェースだけを公開することが重要です。

### P3 — ブラウザによる実操作の回帰テストがCIにない

対象： [.github/workflows/ci.yaml](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/.github/workflows/ci.yaml#L70-L115)

現在はGoのIntegration Test、Vitest、型検査、Lint、ビルドなどが揃っています。

一方、リポジトリ上ではPlaywrightやCypressによるブラウザE2Eテストを確認できませんでした。

今回のアプリは手入力UXが最重要なので、通常の関数単位のテストだけでは保証しにくい動作が多くあります。

特に、スマホの日本語IME、入力フォーカス移動、オフライン復帰、IndexedDBへの保存、複数タブの競合、証拠画像と編集行の連動などです。

改善案： Playwrightで実際のブラウザを操作するE2Eテストを追加してください。

すべてのUIをE2Eでテストする必要はありませんが、少なくとも「品目を連続入力して保存する」「通信を切断して再接続する」「2ユーザーで同時編集する」「AI結果を修正して確定する」の主要経路は自動化したいです。

## 6. 設計上の注意点として残しておきたいもの

次の2点は、一見問題に見えましたが、既存テストに明示された仕様なので、今回の不具合指摘には含めません。

過去月の精算に、加入日より前のメンバーも含める挙動

[settlement/calculation.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/application/settlement/calculation.go#L251-L253) の`memberActiveDuringPeriod()`は脱退日だけを確認しています。

通常の期間判定としては加入日も必要ですが、既存テストでは後から加入したメンバーを過去月に含めることが明示的に期待されています。

後から家計に参加した人に過去の支出を負担させる仕様なら成立します。ただし、その意味をユーザーに明示する必要があります。

Gemini無料枠の429エラーで即座に有料枠へ切り替えない挙動

[ingestion/router.go](https://github.com/walnuts1018/horse/blob/70338cc5822115b2f9a39b7934fe866b0a717a48/internal/ingestion/router.go#L579-L595) では、無料枠がQuota制限に達した際の扱いが制御されています。

既存テストでも即時に有料APIへフォールバックしないことが期待されています。これは無料枠の消費を優先するポリシーとして理解できます。

ただし、ユーザーが待っている即時解析と、バックグラウンド解析で同じポリシーを適用するべきかは、UX上の判断が必要です。

## 修正の優先順位

今回のレビューで特に優先したいのは、以下の順番です。

| 順位 | 対象 | 理由 |
| -- | -------------------- | ----------------- |
| 1 | 入金予定変更と入金義務の不整合 | 精算の正確性に直接影響 |
| 2 | オフライン送信の自動再試行 | 登録したと思った購入が送信されない |
| 3 | 購入履歴全件取得による分析 | データ増加で体験が悪化 |
| 4 | メール取り込みのバッチ容量問題 | 特定条件で取り込みが停止 |
| 5 | メール取り込みの長時間トランザクション | DB接続・ロック・再処理の問題 |
| 6 | ページネーションの可変カーソル | 同時編集で一覧が不整合 |
| 7 | 返金照合の全件取得 | データ増加による性能悪化 |
| 8 | レシート確認・オフライン編集の状態管理 | 入力内容の保存とユーザー体験 |
| 9 | AI検証結果の型付け・UTF-8処理 | 推論の安定性と保守性 |
| 10 | 大きなモジュールの責務整理・E2Eテスト | 将来的な変更容易性 |

このほかのP3指摘も修正価値はありますが、上位の整合性・データ保護・性能問題より優先する必要はありません。

今回確認した範囲では、全体としてGoのApplication Service、GraphQL、PostgreSQLによる整合性管理、ブラウザ側の下書き管理など、主要な構造は整えられています。一方で、正常系を実現する処理は揃っていても、過去の設定変更、大量データ、通信障害、複数人操作などの境界条件で不整合が残る傾向があります。

今後のレビューでも、単純なLint違反やファイル分割だけでなく、異なるモジュール間で前提が食い違う箇所を重点的に調べる価値が高いと思います。

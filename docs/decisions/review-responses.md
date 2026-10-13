# レビュー対応の記録

レビュー文書から完了項目を削除するときは、対応内容と確認根拠をこのファイルか関連する設計判断ファイルに残します。未検証の項目はレビュー文書に残します。

## 2026年10月13日

### バックエンドの責務分離とBob DSL

以前の`docs/reviews/be-architecture.md`にあったGraphQL Mutationの巨大化、購入一覧の関連データ取得と月次・口座集計のN+1、Bob DSLの未活用、会計仕訳構築の重複に関する指摘を確認しました。Mutationは用途ごとに分割し、購入作成・確定や精算などの処理をApplication Serviceへ移しました。購入一覧では関連データをまとめて取得し、月次支出と口座残高では集計をBob DSLへ移しました。会計仕訳生成は共通化しました。該当項目は実装済みとしてレビュー本文から削除しました。

確認箇所は`internal/application/`、`internal/graph/query_purchases.go`、`internal/graph/monthly_spending.go`、`internal/graph/account_balances.go`、`internal/graph/purchase_accounting.go`です。購入一覧の関連データ取得、集計、会計更新を対象にしたDB統合テストを維持します。

購入更新も`internal/application/purchase`のServiceへ移しました。Resolverがトランザクションを所有すると会計期間ロック、購入revisionの競合確認、支払内訳、POSTED購入の仕訳同期、イベント記録がGraphQL層に集まり、別の呼び出し経路で一部だけ実行されるおそれがあるためです。Serviceが同一トランザクション内でこれらを実行し、GraphQL層から注入する操作も同じTxを使います。独立レビューでは権限確認、rollback、会計同期の境界を確認しました。`f68c84e`のGitHub Actions run `38392241924`でGo build、lint、DB統合テスト、Frontend検査、イメージ公開がすべて成功しました。購入更新テストでは同月の旧・新会計期間をロック処理へ渡す2引数契約を検証しています。

複数月の精算previewは、月ごとに精算を計算して同じ基礎データを読み直す構成を改め、未締め期間の拠出義務、予定拠出、訂正、購入由来expenseを範囲単位で読み込み、会計月ごとに計算する構成にしました。購入IDなどの関連読込は引き続き500件単位で分割します。独立の静的レビューで月範囲、終了日を含む予定拠出、終了済み予定の適用月、履歴snapshot、overflow検査を確認しました。締め済み残高を含む月次計算との一致を確認するDB統合テストを追加しましたが、ローカルDB未設定によりskipされました。クエリ数や実行時間の実測はしていないため、性能改善の数値は主張せず、CIのDB統合テスト通過後に複数月読み直しのレビュー項目を解決済みにします。

Resolver内に残る購入訂正などのトランザクションと業務処理、Gemini SchemaとGo型の同期、Query層の配置、DBトレーシング、SSEのポーリングは未完了の指摘として`docs/reviews/be-architecture.md`に残しています。

### 抽出ジョブ基盤

`docs/reviews/be-library.md`にあったPostgreSQLのジョブ管理を廃止してRabbitMQとWatermillへ移す提案は採用しませんでした。PostgreSQLは文書とジョブの原子的な登録、ジョブ状態、再試行、Worker停止後の回復を担います。RabbitMQはWorkerを早く起こす通知に使い、通知を失ってもDB走査で処理を再開します。Watermillを加えてもDBのclaimと回復処理が残るため、責務を減らす効果が確認できませんでした。

採用しない理由、影響、再検討条件は[`extraction-job-queue.md`](extraction-job-queue.md)に記録しています。ライブラリ選定など未判断の提案は`docs/reviews/be-library.md`に残しています。

### SPAの未保存変更とモバイル更新操作

購入・精算・設定・取込の未保存変更を検知し、家庭切替やログアウトなどで変更を破棄する前に確認する処理を追加しました。drawer表示中はbodyを固定し、閉じた後に表示位置を復元します。公開環境で画面URL、直接URL、再読み込み、戻る・進むの動作を確認しました。

変更は`web/src/app/horse-app.tsx`と画面ごとの編集フォームにあります。iOS実機でのpull-to-refresh抑止、履歴操作、深いURLからの再読み込みは未確認のため、`docs/reviews/spa.md`は残します。

ソース監査では、dirty状態に対する`useBlocker`と`beforeunload`、購入下書きのIndexedDB保存、drawer表示中の`overscroll-behavior`とbody固定を確認しました。公開環境の購入入力画面で合成値を入力してホームへ移動しようとすると「未保存の変更を破棄しますか？」という確認が表示されました。入力は送信していません。ブラウザー操作の応答が止まり、確認を閉じた後の状態までは確認できていません。タブ閉じ・更新時の確認とAndroid/iOSのジェスチャーも未確認のため、解決した根拠にはしません。

2026年10月13日に新しい公開イメージで確認し、帳簿への遷移でURLが`?page=ledger`に変わること、`https://horse.walnuts.dev/?page=ledger`の直接表示、再読み込み後の同じ画面維持、戻る・進むによるホームと帳簿の切替えを確認しました。dirtyな購入入力からホームへの遷移では破棄確認が表示され、合成入力は送信していません。ブラウザー操作が応答しなくなったため、合成入力の確認を閉じた後の状態、タブ閉じ・更新、および実機のAndroid/iOSジェスチャーは未確認です。`docs/reviews/spa.md`にはこの未確認項目を残します。

### Dockerと公開イメージ

Go API・Worker・Atlas MigrationとWebを別イメージに分け、依存取得のBuildKit cache mount、main push時のイメージ公開、provenance、SBOMを設定しました。CI run `38392241924`でGo、Web、OCRイメージの公開に成功しました。OCRのソースは変更していないためinfraのOCR digestは維持しました。infra commit `664ff46b3`でAPI・Worker・WebのdigestとWebの正しい`horse-web`リポジトリを反映し、Argo CDが`Synced`・`Healthy`になり4 PodがReadyであることを確認しました。Web実行イメージをpiccaと同じNode 24 distrolessへ切り替えたため、CI公開と公開Podの起動確認までは`docs/reviews/docker.md`にその項目だけを残します。

### GraphQL Schema

mutationごとの入力ファイル分割、Relay契約、状態別型の修正を進めています。GitHub Actions run `38392241924`でgqlgen生成物、Go DB統合テスト、Frontend型検査、Relay artifact検査、FSD検査、lint、ビルドが成功しました。公開UIでページ遷移、URL、戻る・進むを確認しました。`docs/reviews/graphql.md`に残るunion Result、interface/unionの使い分け、nullable削減、Relay接続範囲などは現行SDLとpicca ADRを照合し、未解決項目として継続します。

タグ一覧を`TagConnection`に変更し、`first`/`after`と`last`/`before`によるページングを追加しました。カーソルはタグ名とIDで安定順序を作り、カーソルの家庭スコープと`includeArchived`条件を検証します。Frontendの`readTags`は全ページを取得し、通常一覧ではアーカイブ済みを除外し、設定画面では従来どおり含めます。DB統合テストで前後ページ、アーカイブ条件、別家庭のカーソル拒否を確認し、run `38392241924`でGo DB統合テストとFrontend検査が成功しました。

口座残高分析の`changes`をConnection化し、Frontendは最新100件を初期表示して古い履歴を追加取得する形に変更しました。各行の残高と期間開始・終了残高は全期間の仕訳から計算して返し、ページ範囲だけで残高を再計算しません。レスポンス量を抑える変更であり、DB読取量の削減は主張しません。複数年のページ境界、全ページの行残高、安定順序を確認するDB統合テストがrun `38392241924`で成功しました。

予算一覧を`BudgetConnection`に変更し、期間重複と家庭スコープをカーソルの検証、件数、ページ取得に共通適用します。同名予算は名前とIDで並べ、settingsと分析画面は`readBudgets`で全ページを取得して既存の一覧内容を維持します。期間境界、同名カーソル、前後ページのDB統合テストはDB照合順序への依存を除いた後、run `38392241924`で成功しました。

`PurchaseCorrectionConnection`は`first`/`after`による前方向ページングのみを提供します。帳簿では最初の100件を取得し、`hasNextPage`がtrueなら`endCursor`から最後まで自動取得するため、利用者が訂正履歴を前後に移動する画面はありません。現状で`last`/`before`を追加しても利用経路がなく、サーバー側は親購入の訂正履歴を読み込んでからページを作るため、後方向ページングを追加してもDB読取量を減らせません。訂正履歴を独立してページ移動するUIを追加する場合は、履歴の並び順とDB取得範囲を見直し、前後両方向のConnectionに変更します。この非対称性は意図したAPI判断として記録し、GraphQLレビュー全体の解決や公開検証の根拠には含めません。

### 同時編集と締め後訂正

同一購入の日時変更と店名変更が同時に到着した場合、同じ会計月内ならフィールド別revisionで競合を判定し、両方を反映します。同じフィールドを同時に変更すると後続更新を競合として返します。最初の日時変更で会計月が変わる場合は、最新期間のロックを取り直さないまま更新しないよう競合として返します。会計月をまたぐ別フィールド変更の自動再試行は実装していません。

締め後の購入訂正統合テストに、締め後の品目編集拒否、訂正仕訳の貸借一致、訂正台帳行、購入月分析、次回精算、カード口座残高分析の確認を追加しました。CIでDB統合テストが通過するまでは完了扱いにしません。

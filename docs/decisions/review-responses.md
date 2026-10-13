# レビュー対応の記録

レビュー文書から完了項目を削除するときは、対応内容と確認根拠をこのファイルか関連する設計判断ファイルに残します。未検証の項目はレビュー文書に残します。

## 2026年10月13日

### バックエンドの責務分離とBob DSL

以前の`docs/reviews/be-architecture.md`にあったGraphQL Mutationの巨大化、購入一覧の関連データ取得と月次・口座集計のN+1、Bob DSLの未活用、会計仕訳構築の重複に関する指摘を確認しました。Mutationは用途ごとに分割し、購入作成・確定や精算などの処理をApplication Serviceへ移しました。購入一覧では関連データをまとめて取得し、月次支出と口座残高では集計をBob DSLへ移しました。会計仕訳生成は共通化しました。該当項目は実装済みとしてレビュー本文から削除しました。

確認箇所は`internal/application/`、`internal/graph/query_purchases.go`、`internal/graph/monthly_spending.go`、`internal/graph/account_balances.go`、`internal/graph/purchase_accounting.go`です。購入一覧の関連データ取得、集計、会計更新を対象にしたDB統合テストを維持します。

Resolver内に残る購入訂正などのトランザクションと業務処理、Gemini SchemaとGo型の同期、Query層の配置、DBトレーシング、SSEのポーリングは未完了の指摘として`docs/reviews/be-architecture.md`に残しています。

### 抽出ジョブ基盤

`docs/reviews/be-library.md`にあったPostgreSQLのジョブ管理を廃止してRabbitMQとWatermillへ移す提案は採用しませんでした。PostgreSQLは文書とジョブの原子的な登録、ジョブ状態、再試行、Worker停止後の回復を担います。RabbitMQはWorkerを早く起こす通知に使い、通知を失ってもDB走査で処理を再開します。Watermillを加えてもDBのclaimと回復処理が残るため、責務を減らす効果が確認できませんでした。

採用しない理由、影響、再検討条件は[`extraction-job-queue.md`](extraction-job-queue.md)に記録しています。ライブラリ選定など未判断の提案は`docs/reviews/be-library.md`に残しています。

### SPAの未保存変更とモバイル更新操作

購入・精算・設定・取込の未保存変更を検知し、家庭切替やログアウトなどで変更を破棄する前に確認する処理を追加しました。drawer表示中はbodyを固定し、閉じた後に表示位置を復元します。`?page=`による画面URLは維持し、ブラウザーの履歴と再読み込みの確認を継続します。

変更は`web/src/app/horse-app.tsx`と画面ごとの編集フォームにあります。iOS実機でのpull-to-refresh抑止、履歴操作、深いURLからの再読み込みは未確認のため、`docs/reviews/spa.md`は残します。

### Dockerと公開イメージ

Go API・Worker・Atlas MigrationとWebを別イメージに分け、依存取得のBuildKit cache mountを追加しました。CIによるイメージ公開とinfraへのdigest反映、Argo CD同期後の稼働確認が完了するまでは解決扱いにしません。`docs/reviews/docker.md`を残します。

### GraphQL Schema

mutationごとの入力ファイル分割、Relay契約、状態別型の修正を進めています。型ごとのファイル分割、ブラウザーからの操作、CI、公開環境での動作を確認するまで`docs/reviews/graphql.md`を残します。

タグ一覧を`TagConnection`に変更し、`first`/`after`と`last`/`before`によるページングを追加しました。カーソルはタグ名とIDで安定順序を作り、カーソルの家庭スコープと`includeArchived`条件を検証します。Frontendの`readTags`は全ページを取得し、通常一覧ではアーカイブ済みを除外し、設定画面では従来どおり含めます。DB統合テストでは前後ページ、アーカイブ条件、別家庭のカーソル拒否を確認します。`31b7c68`のCIではDB統合テストを含む他の検査は通過しましたが、`readTags`の型推論がFrontend型検査で失敗しました。明示的な型注釈を加え、ローカル型検査は通過しました。gqlgen生成、Goコードのformatと対象テスト、Feature-Sliced Design検査もローカルで通過しています。修正後のCIと公開環境の確認が残っています。

口座残高分析の`changes`をConnection化し、Frontendは最新100件を初期表示して古い履歴を追加取得する形に変更しました。各行の残高と期間開始・終了残高は全期間の仕訳から計算して返し、ページ範囲だけで残高を再計算しません。レスポンス量を抑える変更であり、DB読取量の削減は主張しません。複数年のページ境界、全ページの行残高、安定順序を確認するDB統合テストを追加しました。統合テストは`HORSE_TEST_DATABASE_URL`未設定のためローカルではskipされており、CI、Frontend型検査、公開環境の確認が済むまでこのレビュー項目は未完了として扱います。

予算一覧を`BudgetConnection`に変更し、期間重複と家庭スコープをカーソルの検証、件数、ページ取得に共通適用します。同名予算は名前とIDで並べ、settingsと分析画面は`readBudgets`で全ページを取得して既存の一覧内容を維持します。期間境界、同名カーソル、前後ページのDB統合テストを追加しました。ローカルの型検査とGraphQL生成は通過しましたが、DB統合テストとCI、公開環境の確認が済むまで未完了として扱います。

`PurchaseCorrectionConnection`は`first`/`after`による前方向ページングのみを提供します。帳簿では最初の100件を取得し、`hasNextPage`がtrueなら`endCursor`から最後まで自動取得するため、利用者が訂正履歴を前後に移動する画面はありません。現状で`last`/`before`を追加しても利用経路がなく、サーバー側は親購入の訂正履歴を読み込んでからページを作るため、後方向ページングを追加してもDB読取量を減らせません。訂正履歴を独立してページ移動するUIを追加する場合は、履歴の並び順とDB取得範囲を見直し、前後両方向のConnectionに変更します。この非対称性は意図したAPI判断として記録し、GraphQLレビュー全体の解決や公開検証の根拠には含めません。

### 同時編集と締め後訂正

同一購入の日時変更と店名変更が同時に到着した場合、同じ会計月内ならフィールド別revisionで競合を判定し、両方を反映します。同じフィールドを同時に変更すると後続更新を競合として返します。最初の日時変更で会計月が変わる場合は、最新期間のロックを取り直さないまま更新しないよう競合として返します。会計月をまたぐ別フィールド変更の自動再試行は実装していません。

締め後の購入訂正統合テストに、締め後の品目編集拒否、訂正仕訳の貸借一致、訂正台帳行、購入月分析、次回精算、カード口座残高分析の確認を追加しました。CIでDB統合テストが通過するまでは完了扱いにしません。

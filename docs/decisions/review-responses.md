# レビュー対応の記録

レビュー文書から完了項目を削除するときは、対応内容と確認根拠をこのファイルか関連する設計判断ファイルに残します。未検証の項目はレビュー文書に残します。

## 2026年10月13日

### バックエンドの責務分離とBob DSL

以前の`docs/reviews/be-architecture.md`にあったGraphQL Mutationの巨大化、購入一覧の関連データ取得と月次・口座集計のN+1、Bob DSLの未活用、会計仕訳構築の重複に関する指摘を確認しました。Mutationは用途ごとに分割し、購入作成・確定や精算などの処理をApplication Serviceへ移しました。購入一覧では関連データをまとめて取得し、月次支出と口座残高では集計をBob DSLへ移しました。会計仕訳生成は共通化しました。該当項目は実装済みとしてレビュー本文から削除しました。

確認箇所は`internal/application/`、`internal/graph/query_purchases.go`、`internal/graph/monthly_spending.go`、`internal/graph/account_balances.go`、`internal/graph/purchase_accounting.go`です。購入一覧の関連データ取得、集計、会計更新を対象にしたDB統合テストを維持します。

購入更新も`internal/application/purchase`のServiceへ移しました。Resolverがトランザクションを所有すると会計期間ロック、購入revisionの競合確認、支払内訳、POSTED購入の仕訳同期、イベント記録がGraphQL層に集まり、別の呼び出し経路で一部だけ実行されるおそれがあるためです。Serviceが同一トランザクション内でこれらを実行し、GraphQL層から注入する操作も同じTxを使います。独立レビューでは権限確認、rollback、会計同期の境界を確認しました。`f68c84e`のGitHub Actions run `38392241924`でGo build、lint、DB統合テスト、Frontend検査、イメージ公開がすべて成功しました。購入更新テストでは同月の旧・新会計期間をロック処理へ渡す2引数契約を検証しています。

複数月の精算previewは、月ごとに精算を計算して同じ基礎データを読み直す構成を改め、未締め期間の拠出義務、予定拠出、訂正、購入由来expenseを範囲単位で読み込み、会計月ごとに計算する構成にしました。購入IDなどの関連読込は引き続き500件単位で分割します。独立の静的レビューで月範囲、終了日を含む予定拠出、終了済み予定の適用月、履歴snapshot、overflow検査を確認しました。`TestSettlementPreviewMatchesMonthlyCalculationsAfterClosedCarry`で締め済み残高を含む月次計算との一致を確認し、CI run `38392241924`が成功しました。クエリ数や実行時間は計測していないため、性能改善の数値は主張しません。

Resolver内に残る購入訂正などのトランザクションと業務処理、Gemini SchemaとGo型の同期、Query層の配置、DBトレーシング、SSEのポーリングは未完了の指摘として`docs/reviews/be-architecture.md`に残しています。

商品候補検索では、Bob DSLによる候補取得、採点、並び替え、関連variant/aliasの取得を`internal/postgres/query`へ移しました。GraphQL層は所属確認、入力検証、GraphQLモデルへの変換を担います。これにより検索条件とDBアクセスをGraphQLから分離しつつ、検索結果と並び順を維持します。専用Query層へGoの生SQL文字列は追加していません。

タグ・定期支出一覧の`includeArchived`と商品候補検索の`first`は、SDL defaultを維持したままnon-null引数にしました。Relay operation variablesと生成物、resolver引数、直接resolverを呼ぶ統合テストを更新しました。既知の呼び出し元は値を渡し、引数省略時にはSDLのdefaultが使われます。外部GraphQLクライアントが明示的なnullを送っていないかは確認できていませんが、nullは契約違反として拒否する判断です。

pgxの`BeginFunc`は採用せず、明示的な`Begin`/`Commit`/`Rollback`を維持します。pgxのcallback関数はRollbackに開始時のcontextを使い、現状の`context.WithoutCancel(ctx)`と同じキャンセル時の挙動を保証できないためです。変更時はトランザクション終了処理の簡潔さより、リクエストキャンセル後にもRollbackを試みる現在の動作を優先します。

### AI購入登録と証拠の原子性

レシート由来の購入登録では、購入・品目・DocumentConfirmation・文書と品目のEvidenceLinkを同じトランザクションで保存します。抽出済み品目数や金額と入力が一致しない場合は購入作成を拒否します。購入を下書きから確定する`postPurchase`は別操作ですが、証拠は購入作成時に記録するため、確定失敗で品目別証拠が欠落しません。

根拠は`internal/graph/accounting_integration_test.go`の`TestAccountingFlowUsesScheduleAndActualFundingSeparately`にある件数・金額不一致の拒否と3件の証拠リンク検査です。実環境でのレシート取込から確定までの操作は`docs/completion-audit.md`に未検証として残します。

### 抽出ジョブ基盤

`docs/reviews/be-library.md`にあったPostgreSQLのジョブ管理を廃止してRabbitMQとWatermillへ移す提案は採用しませんでした。PostgreSQLは文書とジョブの原子的な登録、ジョブ状態、再試行、Worker停止後の回復を担います。RabbitMQはWorkerを早く起こす通知に使い、通知を失ってもDB走査で処理を再開します。Watermillを加えてもDBのclaimと回復処理が残るため、責務を減らす効果が確認できませんでした。

採用しない理由、影響、再検討条件は[`extraction-job-queue.md`](extraction-job-queue.md)に記録しています。ライブラリ選定など未判断の提案は`docs/reviews/be-library.md`に残しています。

### SPAの未保存変更とモバイル更新操作

購入・精算・設定・取込の未保存変更を検知し、家庭切替やログアウトなどで変更を破棄する前に確認する処理を追加しました。drawer表示中はbodyを固定し、閉じた後に表示位置を復元します。公開環境で画面URL、直接URL、再読み込み、戻る・進むの動作を確認しました。

変更は`web/src/app/horse-app.tsx`と画面ごとの編集フォームにあります。公開環境では履歴操作と深いURLからの再読み込みを確認しました。タブを閉じる・更新する場合の未保存確認は`docs/reviews/react.md`に残します。

ソース監査でdirty状態に対する`useBlocker`と`beforeunload`、購入下書きのIndexedDB保存、drawer表示中の`overscroll-behavior`とbody固定を確認しました。2026年10月13日に公開環境で帳簿への遷移によるURL変更、深いURLの直接表示、再読み込み後の画面維持、戻る・進むを確認しました。dirtyな購入入力からホームへ移動すると破棄確認が表示されました。別の合成入力では直接再読み込み後も品名と金額が下書きから復元されましたが、ブラウザー確認は表示されませんでした。合成入力は空に戻し、購入は送信していません。更新・タブ終了時の確認は未解決として`docs/reviews/react.md`に残します。

レビューで指摘された品目では、商品サジェストを選択した直後に自由入力用handlerが消していたvariant IDを、名前変更後に設定するよう順序を直しました。取込画面では、編集中のレシート結果をポーリング応答で上書きしないようにし、明示的な再抽出・購入確定後に保護を解除します。精算画面では世帯と期間を応答snapshotに紐付け、古い応答を破棄し、締めMutationには表示中snapshotのrevisionを使います。各変更の型検査と整形確認は成功し、総合CIは後続のmain実行で確認します。実操作を伴うフロントエンドのtest runnerがなく、小規模なstate更新順序やrequest世代管理だけを検証するモック中心のテストは追加しませんでした。対応した3項目をレビューから削除しました。

### Dockerと公開イメージ

Go API・Worker・Atlas MigrationとWebを別イメージに分け、依存取得のBuildKit cache mount、main push時のイメージ公開、provenance、SBOMを設定しました。CI run `38392241924`でGo、Web、OCRイメージの公開に成功しました。OCRのソースは変更していないためinfraのOCR digestは維持しました。infra commit `664ff46b3`でAPI・Worker・WebのdigestとWebの正しい`horse-web`リポジトリを反映し、Argo CDが`Synced`・`Healthy`になり4 PodがReadyであることを確認しました。Web実行イメージをNode 24 distrolessへ切り替えたあと、Kubernetes側のcommand指定で起動できないことが分かりました。infra commit `92433d001`でcommand指定を削除し、Argo CDが`Synced`・`Healthy`、4 PodがReady、公開URLがHTTP 200であることを確認したため、Dockerの未解決レビュー項目を削除しました。

Renovateが更新できるよう、Go・Web・OCRの公開タグはGitHub Actionsのrun numberをpatch番号にした`v0.0.<run number>`へ統一します。SHAを含む疑似バージョンは使いません。infra commit `16c224ca8`で既存digestに厳密なタグを付け、Webの起動commandがNode distroless imageと合わない問題をinfra commit `92433d001`で修正しました。Horseの次のCIでroute変更を含む新しいdigestを公開し、Argo CDと公開URLの稼働を確認してから、`horse`、`horse-web`、`horse-ocr`の過去のpackage versionsを削除します。稼働中indexが参照するplatform manifestとprovenance/SBOM manifestは削除対象にしません。

### ファイルベースルーティングとURL状態

ページ名を`?page=`へ保存する方式を廃止し、TanStack Startのファイルベースルートで`/households/$householdId/{page}`を構成します。帳簿の検索条件は共有と再読み込みに必要なためsearch paramsへ保存します。画面別ルートは`pages/`のpublic APIを呼び出し、App shellをroot routeに一度だけ配置して未保存変更のblockerと画面状態を維持します。家計取得に失敗して画面が空の場合は、表示中の家計を選び直す操作で取得を再試行します。型検査、Steiger、oxlint、oxfmt、Vite buildを確認しました。新しいimageの稼働確認と直接URL・戻る/進むの公開環境確認はCIとデプロイ後に記録します。

家庭切替中に戻る操作で取得済みの家庭へ復帰した場合、キャンセルされた家庭取得がローディング状態を解除できない競合を修正しました。取得effectが現在の家庭IDと一致する場合にもローディング状態を解除します。

### 追加レビュー対応

GraphQL読取のエラー境界は、ページ遷移で状態をリセットし、再試行時に失敗した画面だけを再マウントするようにしました。共通エラー文言と再試行ボタンを用意し、フルページの再読み込みを使わない構成です。Frontend型検査と整形を確認しました。

会計期間を締める操作に、対象月、全体額、メンバー別金額を確認する画面を追加しました。確認チェックを選択するまで確定ボタンを無効にし、期間変更で確認状態を解除します。Frontend型検査、CSS検査、整形を確認しました。

CSV明細の永続参照は取引番号から行番号へ変更しました。TransactionIDは照合属性に保ち、同一取引番号の複数行を別々に確定・冪等再試行する統合テストを追加しました。購入訂正は品目別と購入全体の両方の残額を検証し、全体返金後に追加返金できない統合テストを追加しました。BobのDSLで関連データを読み、合計値のoverflowも拒否します。非DBの取込照合テストは成功しました。DB統合テストは`HORSE_TEST_DATABASE_URL`未設定のためローカルでは実行しておらず、GitHub Actionsで実行します。

取り込み画面は最大3ファイルを並列送信し、個別失敗をキューに残して再送できるようにしました。認識完了を待って次の原本を送る直列処理を廃止しました。原本のファイル名はDBへ保存し、アップロード応答・一覧・詳細APIで返します。店舗・取り込み日・原本種別・確定状態による絞り込みを追加し、カーソルを世帯と全条件へ紐付けました。店舗検索はレシートと明細CSVの両方に対応します。Go APIテストとFrontend型検査が成功しました。

購入訂正の精算調整は購入時点の支払元ルール、プール負担スナップショット、資金拠出ルールから再計算します。後続のルール変更後に返金訂正しても過去の設定を使うDB統合テストを追加しました。購入全体と品目の両方で返金上限を検証します。Testcontainers PostgreSQL 18で`TestPostedPurchaseCorrectionAffectsNextSettlementWithoutRewritingClosedPeriod`と`TestCorrectPurchaseFromStatementKeepsRowEvidenceAtomic`を実行し、締め後の訂正、次回精算、返金上限、明細証拠の原子的保存を確認しました。

### GraphQL Schema

Mutation単位とドメイン型単位でSDLを分割し、gqlgenは`follow-schema`を使います。各Mutationは`Success | UserError`のResult unionを返し、共通`MutationError` interfaceを使います。Mutation Successにentity IDを含める場合は`@goField(forceResolver: true)`と`encodeNodeID`でRelay Global IDを返します。Ledger、Settlement、Pool、Observation、PurchaseCorrection、AccountBalanceSourceも型の違いをinterface/unionで表しています。この構成はpiccaのADR 0002とSDL分割に沿うため、該当するレビュー指摘は解決済みとして`docs/reviews/graphql.md`から削除しました。根拠は`gqlgen.yml`、`graphql/*_mutation.graphql`、各domain SDLです。

nullable outputの静的監査では、明白にnon-nullへ変更できるfieldは見つかりませんでした。任意の関連ID・終了日・商品数量・OCRの観測日時など、nullableが値の欠損条件を表す箇所は維持します。default付きの`includeArchived`と`productSuggestions.first`はSDLでnon-nullに変更し、Relay operation variablesとResolver引数も更新しました。`ProductVariant.brand`と`packageSize`は未設定値をnullへ統一しました。`Budget`、`Tag`、`RecurringExpense`、`ProductCatalog`の一覧はConnection化し、Frontend read関数で全ページを取得します。これらのレビュー項目は解決済みです。

タグ一覧を`TagConnection`に変更し、`first`/`after`と`last`/`before`によるページングを追加しました。カーソルはタグ名とIDで安定順序を作り、カーソルの家庭スコープと`includeArchived`条件を検証します。Frontendの`readTags`は全ページを取得し、通常一覧ではアーカイブ済みを除外し、設定画面では従来どおり含めます。DB統合テストで前後ページ、アーカイブ条件、別家庭のカーソル拒否を確認し、run `38392241924`でGo DB統合テストとFrontend検査が成功しました。

口座残高分析の`changes`をConnection化し、Frontendは最新100件を初期表示して古い履歴を追加取得する形に変更しました。各行の残高と期間開始・終了残高は全期間の仕訳から計算して返し、ページ範囲だけで残高を再計算しません。レスポンス量を抑える変更であり、DB読取量の削減は主張しません。複数年のページ境界、全ページの行残高、安定順序を確認するDB統合テストがrun `38392241924`で成功しました。

予算一覧を`BudgetConnection`に変更し、期間重複と家庭スコープをカーソルの検証、件数、ページ取得に共通適用します。同名予算は名前とIDで並べ、settingsと分析画面は`readBudgets`で全ページを取得して既存の一覧内容を維持します。期間境界、同名カーソル、前後ページのDB統合テストはDB照合順序への依存を除いた後、run `38392241924`で成功しました。

`PurchaseCorrectionConnection`は`first`/`after`による前方向ページングのみを提供します。帳簿では最初の100件を取得し、`hasNextPage`がtrueなら`endCursor`から最後まで自動取得するため、利用者が訂正履歴を前後に移動する画面はありません。現状で`last`/`before`を追加しても利用経路がなく、サーバー側は親購入の訂正履歴を読み込んでからページを作るため、後方向ページングを追加してもDB読取量を減らせません。訂正履歴を独立してページ移動するUIを追加する場合は、履歴の並び順とDB取得範囲を見直し、前後両方向のConnectionに変更します。この非対称性は意図したAPI判断として記録し、GraphQLレビュー全体の解決や公開検証の根拠には含めません。

### 同時編集と締め後訂正

同一購入の日時変更と店名変更が同時に到着した場合、同じ会計月内ならフィールド別revisionで競合を判定し、両方を反映します。同じフィールドを同時に変更すると後続更新を競合として返します。最初の日時変更で会計月が変わる場合は、最新期間のロックを取り直さないまま更新しないよう競合として返します。会計月をまたぐ別フィールド変更の自動再試行は実装していません。

締め後の購入訂正統合テストに、締め後の品目編集拒否、訂正仕訳の貸借一致、訂正台帳行、購入月分析、次回精算、カード口座残高分析の確認を追加しました。CIでDB統合テストが通過するまでは完了扱いにしません。


### 2026年10月13日の追加対応

ホーム画面の直近7日支出と当月品目数を`homeSummary`で集計し、期間と日付を日本時間に統一しました。最近の購入8件は一覧表示にだけ使用します。Goビルド、Relay生成物検証、Frontend型検査が成功しました。

入金と送金は送信内容と冪等キーをlocalStorageへ保存し、通信失敗や再読み込み後も同じ入力を再送します。成功時は相手、金額、対象月を表示します。Frontend型検査が成功しました。DB統合テストは`HORSE_TEST_DATABASE_URL`未設定のため未実行です。

分析画面は購入履歴の全ページ取得後に集計し、取得失敗時は不完全な数値を表示せず再試行を提示します。新規品目のカテゴリは未分類を初期値とし、利用者の選択や確かな商品・履歴候補を維持します。Frontend型検査が成功しました。

取り込み一覧に古い履歴の追加取得を実装し、ポーリング後も取得済みページを保持します。検索条件はTanStack Routerのsearch stateへ保存し、戻る・進むで復元します。手動fetchとpollingの整理は`docs/reviews/react.md`に残しています。

仕訳明細の一括登録をBobの複数行INSERTへ変更しました。`go test ./internal/application/accounting`が成功しました。

初回表示時にsearch paramsを正規化する処理が子ルートをホームへ戻すため削除しました。公開環境で深いURLの再読み込み、戻る・進むを確認する必要があります。

### 2026年10月13日のレビュー整理

`docs/reviews/all.md`の購入全体訂正における負担比率比較は、入力比率と保存済み金額比率を最大公約数で正規化して比較するよう変更しました。異なる比率、異なるプール、重複プール、不正な保存値を拒否する単体テストを追加し、`internal/graph`のテストとlintが成功したため、該当項目をレビューから削除しました。

Geminiの未知モデル価格は`unknown_model_price`として明示し、有料ルートで価格未登録モデルを呼び出す前に拒否するようにしました。無料ルートの試行は維持し、未知モデルの拒否と費用メタデータを検査するテストを追加しました。`internal/ingestion`のテストとlintが成功したため、該当項目をレビューから削除しました。

`docs/reviews/be-architecture2.md`のUUID変換とApplication共通エラー分類は、入力UUIDのparse errorを`ErrBadInput`として返し、共通の`apperror`分類をGraphQL結果へ変換する実装に整理しました。購入作成・確定・更新の不正IDと共通エラー分類を検証する単体テストを追加し、対象Goパッケージのテストとlintが成功したため、同文書の「型とエラーの扱い」節を削除しました。

`docs/reviews/react.md`にあった取り込み確定時の複数決済・品目カテゴリ・商品variant選択、モバイル文字サイズ、タップ領域、購入入力順序、通知種別、原本と編集欄の配置、TanStack Routerの`Link`による画面遷移、初回設定ガイドを実装しました。商品variantは`productSuggestions`で候補を検索し、選択IDを購入作成へ渡します。初回設定ガイドは既存の家庭データと購入件数から未設定項目を判定し、負担先・決済元・標準配分の該当sectionへ移動します。確認した箇所は`web/src/pages/imports/ui/imports-page.tsx`、`web/src/pages/purchase-create/ui/purchase-create-page.tsx`、`web/src/pages/home/ui/home-page.tsx`、`web/src/pages/home/ui/setup-guidance.tsx`、`web/src/app/horse-app.tsx`、`web/src/app/styles/global.css`、`web/src/shared/ui/notice.tsx`です。Frontendの型検査、FSD、lint、formatが成功しました。ホームの7日間支出グラフには日付と金額の代替ラベルを追加しました。

購入下書きのIndexedDB保存失敗時に「保存を再試行」を表示し、成功時だけ保存時刻を更新して未保存状態を解除します。`web:check`、対象lint・format、FSD検査が成功したため、UXレビュー#13を削除しました。取り込み画面には不確実な品目・金額・決済差異の確認キューを追加し、前後操作で該当入力へ移動します。`web:check`、FSD、対象lint・formatが成功したためUXレビュー#18を削除しました。補足文字の色とサイズを変更し、`#f3f6f2`上のコントラスト比を1.81:1〜4.46:1から4.99:1へ改善しました。白と確認した薄色背景でも4.80:1以上を確認し、タップ領域を44px以上にしました。stylelintと`git diff --check`が成功しました。
GitHub Actions run `38415764085`全体は購入検索統合テストのfixture不備で失敗しました。同run内の`TestPurchaseRefundSettlementUsesOriginalFundingSnapshotAfterRuleChanges`、`TestPostedPurchaseCorrectionAffectsNextSettlementWithoutRewritingClosedPeriod`、`TestAccountingFlowUsesScheduleAndActualFundingSeparately`は成功し、購入時ルール変更後の訂正と負担ルール一覧の一括読込を確認しました。同runのFrontend型検査、Relay検査、lint、FSD、format、buildも成功しました。

run `38417152110`では、全購入に同じデフォルト負担先を使ったため、専用プールfilterの期待値が実データと合わず購入検索統合テストが失敗しました。別プールを作り、対象購入以外の品目へ明示配分して再実行します。

UXレビュー#14では、撮影後の画像確認、90度回転、矩形トリミング、ダイアログのフォーカス管理、Escapeキー終了、キーボード用切り抜き調整を追加しました。#15では設定画面を「口座」「負担」「分類」「予算」「メンバー」のタブへ分け、選択中の領域だけを表示します。予算や資金移動をトップレベル画面へ移す案は、経路構成と利用頻度を確認して別途判断します。#16の検索語・期間・分類・タグ・決済元・プールによる購入絞り込みはBob DSLでDB適用後にページングするためレビューから削除しました。#17では購入一覧の次ページ取得エラーに条件付きの再試行を加え、検索条件が変わった場合は古いエラーを表示しないようにしました。#19ではモバイルで見える補足・行・チャートラベルを10px以上、主要操作のタップ領域を44px以上にしました。ドロワーのoverscrollとスクロール連鎖は既存CSSで抑止しています。Web型検査、FSD、対象lint・format、Stylelintに成功しました。

取り込み画面は最大3件を並列送信し、各原本の送信状態と失敗時の再送を表示します。原本一覧はカーソルを使って古い履歴を追加取得し、フィルターを変更した時点で以前の一覧とcursorを破棄します。実装済みのためUXレビューの並列化・進捗とページネーション項目を削除しました。

## 2026年10月14日

`docs/reviews/be-architecture.md`と`docs/reviews/be-architecture2.md`を現行コードに照合しました。購入更新で既存負担スナップショットを保持し、新規配賦には現在のルールからスナップショットを作成します。決済内訳の更新時は`purchasePayments`が支払元スナップショットを再作成し、不整合がある場合は処理を失敗させます。`TestPostedPurchaseUpdatesPreserveExistingSnapshotsAndCreateCurrentSnapshotsForNewAllocations`、`TestPurchaseRefundSettlementUsesOriginalFundingSnapshotAfterRuleChanges`、`TestPostedPurchaseCorrectionAffectsNextSettlementWithoutRewritingClosedPeriod`で関連する更新後精算を検証しています。したがって、購入確定・更新・訂正の会計同期とスナップショット欠落を最優先課題としていた旧指摘をレビューから削除しました。

精算の期間範囲は`calculatePeriodsAccounting`がまとめて読み込み、複数月プレビューで同じ基礎データを月ごとに再取得しません。`TestSettlementPreviewMatchesMonthlyCalculationsAfterClosedCarry`で締め済み期間からの繰越結果を検証しています。N+1とResolver配下に精算処理が残るという旧指摘を削除しました。

月次支出と商品候補は`internal/postgres/query`へ移動し、PostgreSQL Poolには`otelpgx.NewTracer()`を設定しました。予算・定期支出の作成、更新、アーカイブ処理を`internal/application/planning`へ移し、Resolverには認可とGraphQL変換を残しました。既存の`TestPlanningMutationsAreAtomicAndHouseholdScoped`を含むApplicationパッケージのテストとGraphQLパッケージの通常テストが成功しました。DB統合テストは`HORSE_TEST_DATABASE_URL`未設定でskipされており、PostgreSQLを使うCIの成功後に統合経路を確認します。

購入訂正と品目更新を`internal/application/purchase`へ移し、Resolverには認可・入力変換・結果変換を残しました。口座残高分析の残高計算とBob DSL読取も`internal/postgres/query`へ移しました。`go test ./internal/application/purchase ./internal/postgres/query ./internal/graph`が成功しました。Queryパッケージに専用テストはありません。GraphQLパッケージの既存`TestAccountTransferFlowsThroughAccountMovementsAndBalanceAnalysis`は残高分析の利用経路を確認します。これら3項目は実装済みとしてレビュー本文から削除しました。Query層は引き続き口座の全仕訳を読み、読込量や応答時間の計測はしていないため、性能改善を達成したとは記録しません。

口座間移動と外部入金のトランザクション、Bob DSLによる永続化、冪等性処理を`internal/application/accountmovement`へ移し、GraphQL Resolverには認可と入出力変換を残しました。`go test ./internal/application/accountmovement`と、Worker分割の修正後に実行した`go test ./internal/graph`は成功しました。Applicationパッケージには専用テストがありません。GraphQLの`TestAccountTransferFlowsThroughAccountMovementsAndBalanceAnalysis`などDB統合テストは`HORSE_TEST_DATABASE_URL`未設定でskipされており、GraphQLからDBまでの統合確認はCIのPostgreSQL統合テスト成功後に完了扱いとします。

処理が混在していた取り込みWorkerを、`worker.go`の進行制御、`worker_claim.go`のジョブ取得、`worker_extract.go`の抽出、`worker_history.go`の履歴参照、`worker_results.go`の成功・失敗保存、`worker_recovery.go`の期限切れジョブ回収へ分割しました。DB管理と抽出処理を分けるためにRepositoryやPipelineの抽象層を増やすのではなく、責務ごとに同じ`Worker`実装内のファイルを整理しました。新しいテストは追加していません。`mise exec -- go test ./internal/ingestion`と`git diff --check`は成功しました。

Workerの挙動は従来のPostgreSQLジョブ管理を維持します。Claim対象は`QUEUED`、`failure_count < 5`、`available_at <= now`を満たすジョブで、`FOR UPDATE SKIP LOCKED`と`available_at`・ID順を使います。再試行可能な失敗は既存の`retryDelay`を適用して再登録し、Gemini Spend Capは失敗回数を増やさず再試行します。35分以上更新されていない`RUNNING`ジョブは回収し、失敗回数を加算して再登録または`retry_exhausted`にします。完了・失敗更新は実行中状態と`attempt`を照合し、古いWorkerによる結果上書きを防ぎます。Watermillなど新たなジョブ基盤を追加せず、DB claim・再試行・回復の既存条件を保つ判断は過去の記録どおりです。

DBアクセスはBob DSLを使う方針を維持します。Bob Query Code Generationの提案は生SQL文字列を導入するため採用せず、複雑な読取もBob DSLで表現します。Bob Factoryは無効のままです。会計値の暗黙化を避け、重複削減の具体的な必要が確認できたFixtureだけ検討します。

GraphQLレビューの一覧取得項目を確認しました。`Budget`、`Tag`、`RecurringExpense`、`ProductCatalog`はRelay Connectionを返し、対応するFrontend read関数は`hasNextPage`を見ながら全ページを取得します。設定画面で各一覧から項目を選択・編集し、商品やタグは購入入力でも候補全体を使うため、read関数で全ページを読む要件は維持しました。APIのConnection化と既存順序を保ったページ継続が完了したため、レビュー本文から削除しました。

`ProductVariant.brand`と`packageSize`は未設定をnullで表す契約に揃えました。単一variant resolverは空文字をnullへ変換していましたが、allocation ruleとbatch catalog resolverは空文字を返していました。該当するBob DSL結果の変換も同じoptional mapperを使うようにし、GraphQLレビューから項目を削除しました。

OCR照合はGeminiが返した品目だけでなく、信頼度0.5以上で金額と文字を含み、品目領域に対応しないOCR行も検出します。税・合計・支払・割引・日付などの明細以外の行は候補から除き、該当時は確認事項として返します。`TestOCRCorroborationFindsUnmatchedItemRows`で読み飛ばし候補の検出と小計行の除外を確認しました。

SDL分割は、`LedgerEntry`のinterface/Connection、各具象ledger型、`PurchasePayment`を独立したドメイン型ファイルに分けました。`createPurchase`と`createAndPostPurchase`も各Mutation操作のSDLファイルに分け、両方で使うInput型は`create_purchase_input.graphql`へ配置しました。`AccountBalanceSource` unionのメンバーは分割後も同名型として解決されます。Relayとgqlgenの生成後にGo GraphQLパッケージテストを実行しました。

UXレビューの未分類カテゴリ指摘は、履歴・商品候補・復元下書き・貼り付け・購入複製から取得するカテゴリIDを有効カテゴリに照合し、無効またはアーカイブ済みなら未分類へ正規化して解決しました。日別支出はPOSTED/CLOSED購入を東京時間の購入日に集計し、返金を元の購入日に反映する既存会計規則に一致します。東京時間の日付境界、下書き除外、返金反映、7日分の出力を検証する統合テストを追加しました。

### 保守性・シンプルさレビューの現行コード照合

`docs/reviews/simple.md`をコミット`1433e4e`のコードに照合し、現行コードで解消している記述を削除または限定しました。購入更新以外のApplication Service移行を反映し、購入更新でResolverから`UpdateOperations`を注入している課題を記録しました。購入検索の主要条件はサーバー側でページング前に適用されるため、全件取得して画面で絞るという旧指摘を削除しました。金額条件の画面内適用と購入・品目表示の重複は残しています。口座移動と外部入金はApplication Serviceへ移りましたが、初期残高・口座移動・外部入金の仕訳行は個別に作成されるため、仕訳生成の重複は未解決としました。画面行数と`useState`数、global.css行数、Frontendテストの有無は当時のコードから再計測しました。

購入更新と購入品目更新から`UpdateOperations`・`UpdateItemOperations`を削除し、会計期間ロック、支払更新、資金負担snapshot維持、仕訳・ledger同期、商品別名更新をPurchase Application Service内のBob DSL処理へ移しました。Resolverは認証、入力変換、Service呼び出し、結果変換を行います。`docs/reviews/simple.md`から解消した指摘を削除しました。対象Goテストとgolangci-lintは成功し、DB統合テストは`HORSE_TEST_DATABASE_URL`未設定のためローカルではskipされます。新規コードに生SQL文字列はありません。

購入の通常登録とAI取込は`createAndPostPurchase`一つで購入作成・記帳まで実行します。購入Application Serviceは同じDBトランザクション内で作成・証拠保存・記帳を行い、Frontendの`create-and-post.ts`はこのMutationを呼びます。レシートの品目観測とカード明細の`sourceReference`による文書確認・証拠リンクもこのトランザクションに含めます。statementの`sourceReference`経路は`TestCorrectPurchaseFromStatementKeepsRowEvidenceAtomic`をTestcontainers PostgreSQL 18で実行し、返金訂正、DocumentConfirmation、原本領域・抽出内容の保存が原子的であることを確認しました。既存購入へ別の原本を後から追加する`/purchases/{id}/evidence`は独立した証拠グルーピング操作として維持し、購入登録後の後続処理には使いません。`docs/reviews/simple.md`の購入作成・記帳の分割指摘は解消済みとして削除しました。

レシート観測保存とHouseholdロック順の更新後に`TestCreateAndPostPurchaseIsAtomicAndIdempotent`をTestcontainers PostgreSQL 18で実行し、POSTED状態、revision、冪等再試行、レシート品目証拠、記帳失敗時のrollbackを確認しました。statementの返金訂正と明細証拠の原子性は`TestCorrectPurchaseFromStatementKeepsRowEvidenceAtomic`で別途確認しています。対象範囲のGoテストとgolangci-lintも成功しました。

GitHub Actions run `38451243233`ではPostgreSQL統合テストが実行され、購入作成時の品目証拠リンクを検証する既存テストが、削除済みの汎用リンクも期待していたため失敗しました。現行の契約は抽出品目ごとのEvidenceLinkのみを作成するため、テストの期待数を購入品目数に合わせました。修正後はTestcontainers PostgreSQL 18で購入登録と返金訂正の対象統合テストが成功しました。GitHub Actionsの修正後runは確認していません。

購入一覧のRelay fragmentは`mapPurchaseItem`でEntity型へ変換し、`as unknown as PurchaseItem[]`を削除しました。mapperは生成されたPurchaseList fragmentの品目型を入力に取ります。この具体的な指摘は解決済みとして`docs/reviews/simple.md`から除き、手書きのHousehold API型、`normalizeGraphQLValue()`、Mutation名を使う`normalizePurchaseVariables()`は残る課題として維持しました。

Household Entityへ家計一覧・詳細のRelay read APIと作成・招待受け入れMutationを移しました。生成型を明示mapperへ通し、pool種別、member role、負担率、default allocationをdomain型へ変換します。Mutationはsuccess、`MutationError`、未知の`__typename`を分岐し、画面から`executeGraphql<T>`と手書きresponse型を削除しました。未使用になった`executeGraphql()`も削除しました。`useGraphqlMutation()`が他のMutation unionを展開するため、共有の`normalizeGraphQLValue()`とunwrap処理は維持しました。型検査、FSD検査、Relay検証、対象ファイルformat検査が成功しました。

招待管理・タグ・予算・定期支出・口座残高推移のUIと状態を設定ページから別componentへ分け、GraphQL operationも`pages/settings/api`へ整理しました。どれも設定ページだけで使うため、`pages/settings/ui`内に置き、再利用されないfeature sliceを作りません。品目入力も同様に`pages/purchase-create/ui/edit-purchase-items.tsx`へ分け、状態と行更新を親ページから移しました。Importsの撮影・プレビュー・回転・切り抜きも`pages/imports/ui/camera-capture.tsx`へ移し、親ページにはupload queueへの追加を残しました。画像座標変換は`pages/imports/model/evidence-geometry.ts`へ分離しました。共有API生成型の参照は`shared/api/index.ts`から公開します。

口座の初期残高、口座移動、外部入金の借貸仕訳は`accounting.BuildEntries()`と`InsertEntries()`を利用するように統一しました。既存の`internal/application/accounting`テストが仕訳の釣り合いと不正行を検証するため、同じ不変条件を重ねるテストは追加していません。

購入Queryの検索条件を`PurchaseFilter`入力に集約しました。金額・日付・検索語・分類・タグ・口座・負担先をページング前にBob DSLで適用し、品目表示では条件を同一品目へ適用して該当品目だけを返します。LedgerPageの購入・品目結果に対する重複filterを削除しました。GraphQL/Relay生成、GraphQL通常テスト、Web型検査、Relay検証、format検査が成功しました。PostgreSQL統合テストの実行結果はGitHub Actions確認後に記録します。

Purchaseの履歴QueryをRelay生成operationへ移し、Purchase Entityのmapperで数量・印字単価・実効単価と訂正unionを変換します。購入入力と観測照合はEntityの公開read APIを利用するため、`normalizeGraphQLValue()`のPurchaseItem変換を削除しました。LedgerEntry変換とmutation unionのunwrapは利用箇所が残るため維持します。生成型を`shared/api`から公開し、Entityのdeep importを解消しました。Web型検査、Steiger、Relay検証、lint、formatの統合検査が成功しました。

会計期間のJST変換と符号付き整数の加算overflow検知を`internal/application/accounting`へ集約しました。購入・資金拠出・送金・入金・精算は共通関数を呼び出します。JSTの月境界と加算overflowを確認するテストを追加し、対象Applicationのテストとgolangci-lintが成功しました。

### 2026年10月14日のレビュー対応

`CreateTag`のタグ名検証、household行ロック、Bob DSL insert、`tag.created`イベント記録、transactionを`internal/application/tag`へ移しました。GraphQL Resolverには認可、Service呼び出し、結果変換を残しました。`go test ./internal/application/tag ./internal/graph -v`と`mise run lint`が成功しました。`HORSE_TEST_DATABASE_URL`が未設定のため、既存の`TestTagConnectionPaginatesWithinHouseholdAndFiltersArchivedTags`を含むGraphQL DB統合テストはskipされています。更新・アーカイブ操作など、Resolver内にtransaction境界が残るMutationは未解決としてレビューに残します。

招待の発行・失効を設定pageのAPIへ移し、Relay生成Mutation型と結果unionの明示的な分岐を使うようにしました。招待一覧のread pathと合わせ、設定UIから`readGraphql<T>`とunsafe castを除去しました。発行tokenの表示、一時IDの保持、一覧更新順は維持しています。型検査、FSD検査、lint、format、Relay検証が成功しました。

カテゴリ、標準負担割合、口座、資金拠出ルール、負担ルール、商品、入出金、予算、タグ、定期支出の設定Mutationをpage APIへ移しました。Relay生成variablesとresponse型を使い、success、`MutationError`、未知の結果を明示的に処理します。精算の入金、送金、返済、期間締めも生成Mutation型へ移し、snapshot revisionと成功後の再取得順を維持しました。未使用になった共有`readGraphql<T>`を削除しました。

Ledgerの品目一括編集は選択、適用、Undoをpage内のmodelとUIへ分離しました。revision競合時の一括rollbackとfilter変更時のselection解除は維持し、既存の一括更新統合テストを実行しました。購入の競合再取得はRelay生成`PurchaseHistoryQuery`とPurchase Entityのmapperを使い、100件単位のcursor走査をoperation変数へ反映しました。Importsのレシート確認フォームとLedgerの数量・印字単価・正規商品の編集フォームを、それぞれ同じpage sliceの専用UIへ移しました。今回の変更後にWeb型検査、FSD検査、format検査が成功しました。

購入証拠の取得状態と表示をLedgerの同sliceにあるAPI・model・UIへ分離しました。画像・PDF表示、品目ハイライト、取得エラーを維持し、`ledger-page.tsx`は1,775行から1,695行になりました。取込検索条件はURLクエリから復元し、適用、戻る、進むで同期します。原本ファイル名はAPIが返す保存値を優先します。

予算、タグ、定期支出の一覧read operationをRelay生成型と明示mapperへ移しました。手書きConnection型と`readGraphql<T>`を除き、金額とenumの変換を各mapperで明示します。未知の定期支出cadenceは暗黙変換せずエラーにします。

モバイルのroot overscrollを抑止し、撮影確認dialogのスクロール境界を閉じました。未保存離脱guardとdrawer内overscroll抑止は既存実装を確認しました。撮影dialogのフォーカス初期化、Tab循環、Escape、復帰、ARIA属性も既に実装されていたため、重複実装は追加していません。

runtimeの`internal/**/*.go`を監査し、直接のSQL文文字列、`psql.Raw`、Bob以外からの`Query`・`Exec`・`Prepare`呼び出しがないことを確認しました。GraphQL filter統合テストは専用PostgreSQLで成功し、既存fixtureの件数と順序を正しく反映しました。会計fixtureの1円購入は負担先poolを固定してUUID順に依存する端数差を除きました。Webの型検査、FSD、lint、format、Relay検証、本番buildと差分検査が成功しました。

商品カタログ一覧、商品候補、残高一覧と履歴、招待一覧、ホームの概要・月間支出・精算結果、分析の月間支出・メンバー別支出、Settlement・貸付残高、未照合観測をpage/entity所有のAPI・mapperへ移しました。Relay生成型を入力にし、手書き通信型と対象read pathの`readGraphql<T>`を除去しました。画面のfallback・pagination・更新依存を維持しました。残る`readGraphql<T>`利用はMutationです。

設定画面専用のレイアウト、section、行、負担設定、招待リンクのstyleと、購入作成専用の見出し、詳細フォーム、outboxのstyleを各pageのCSS moduleへ移しました。設定画面以外でも使う招待prompt、件数badge、avatarの基本形と購入・帳簿間で共有する入力styleはglobal stylesheetに残しました。`global.css`は4,392行です。

HouseholdEventのJSON化とBob insertを`accounting.RecordEvent`へ集約し、購入・入金・振替・planningから呼び出しました。冪等性検証は各操作でfingerprintとresourceの契約が異なり、同じ処理へまとめると契約を曖昧にするため、個別に維持します。対象ApplicationのGoテストと`golangci-lint`が成功しました。

2026年10月14日に合成PNGをPaid Projectの`gemini-3.8-flash`へ1回送信しました。応答は店名「合成テスト店」、品目2件、8%税情報、合計378円を含み、modelVersionは`gemini-3.8-flash`でした。usage metadataは入力1,731token、出力295tokenです。公式のStandard料金を用いた推定額は$0.0024045（当時のアプリ為替158.535円/$で約0.38円）で、実請求額ではありません（[Gemini API料金表](https://ai.google.dev/gemini-api/docs/pricing)）。合成画像と一時Job、Pod、ServiceAccount、NetworkPolicyは削除し、APIキー値は読み取らず出力していません。

### 保守性・シンプルさレビューの画面分割とRelay読み込み

設定、取込、帳簿、購入作成、精算の画面から機能別UIと状態を同じ`pages`slice内へ分けました。画面固有の機能で再利用先がないため、独立した`features`sliceは追加しません。設定の口座活動・配賦ルールと購入履歴はRelay生成operationを使い、各所有sliceのmapperで画面・Entityの型へ明示的に変換します。口座活動mapperはQueryが返す口座の`id`と`name`だけを扱い、未取得のフィールドを補いません。購入履歴の`PurchaseItem`汎用変換は削除し、LedgerEntry変換とMutation結果の共通処理は既存利用が残るため維持します。

レビューの画面行数と`useState`呼び出し数を現在の実装に合わせて更新しました。Web型検査、Steiger、Relay検証、対象lint・formatが成功しました。残る共通GraphQL変換と手書き通信型は`docs/reviews/simple.md`に維持しています。

`SetFundingSchedule`のトランザクション、旧scheduleの終了と無効化、新schedule作成、イベント記録を`internal/application/funding_schedule`へ移しました。Resolverは認証、日付変換、Service呼び出し、GraphQL結果変換を行います。Application Service内のDB操作はBob DSLを使用します。

`CreatePool`のトランザクション、プールと初期資金拠出ルールの作成、既定配賦versionの複製と更新、イベント記録を`internal/application/pool`へ移しました。Resolverは認証とGraphQL結果用のhousehold再読込を行います。プール追加時に既存配賦を複製して新しいプールへweight 1を加える動作を維持しました。

`CreateAccount`の認可後処理、口座作成、開始残高の仕訳とイベント記録を`internal/application/account`へ移し、口座と仕訳を同じtransactionで保存します。`CreateCategory`の認可後処理、カテゴリ作成とイベント記録も`internal/application/category`へ移しました。DBアクセスはBob DSLを使用します。

### 2026年10月14日の画面責務分割とCSS

分析・帳簿・取り込み・購入登録・精算画面の取得状態、入力、送信、集計、締め処理を、それぞれの`pages`slice内のhookとUIへ分割しました。単一画面のためだけに使う処理は共有`features`へ移しません。`AnalysisPage`は647行から247行、`LedgerPage`は1,148行から660行、`ImportsPage`は756行から398行、`PurchaseCreatePage`は498行から359行、`SettlementPage`は287行から226行になりました。精算画面の`useState`は8件から3件になりました。分析・購入登録・帳簿・取り込み・口座残高履歴だけで使うCSSを各page stylesheetへ移し、共有規則は`global.css`に残しました。`global.css`は2,539行です。Web型検査、FSD検査、lint、format、production buildが成功しています。

### 2026年10月14日のMutation Service移行

Resolverに残っていた世帯作成、デフォルト配賦・配賦ルール、プール資金拠出ルール、招待の発行・取消・受諾、商品概念・バリアント・別名、一括購入・品目更新をApplication Serviceへ移しました。カテゴリとタグの作成・更新・archiveもService経由です。DB書き込みはBob DSLを使い、認可、revision競合、会計期間lock、会計状態の更新、event記録を各ユースケースのtransactionに含めています。ResolverにはGraphQL値の変換、認可後のService呼び出し、結果の再読込を残しました。

`TestSetDefaultAndRulePersistVersionsWeightsAndEvents`、`TestSetFundingRuleValidatesAndVersionsHouseholdRule`、`TestPlanningMutationsAreAtomicAndHouseholdScoped`、`TestBulkUpdatePurchasesMergesDisjointFieldsAndRollsBackConflicts`、`TestHouseholdInvitationLifecycle`、`TestProductCatalogAndSuggestionsPreserveExpectedOrdering`、カテゴリ・タグの統合テストを、最新migrationを適用した一時DBで確認しました。一時DBはテスト後に削除し、既存の`horse`DBを変更していません。実行時Goコードを調べ、Bob DSLを経由しないSQL文字列のDB呼び出しがないことも確認しました。

### 2026年10月14日のライブラリ導入

`picca`を参照し、`echo/v5` v5.3.1をAPI・WorkerのHTTPルーティングへ導入しました。HTTP serverのtimeout、SSE、標準`http.Handler`との接続、GETに対するHEADを維持します。`kessoku` v1.1.1はAPI・Worker起動時の依存構築に使い、generator sourceとgenerated fileを各packageの`kessoku.go`・`kessoku_band.go`へ揃えました。`testcontainers-go` v0.38.0はGraphQL統合テストでPostgreSQL 18を起動し、既存Atlas migrationを適用します。CIから`HORSE_TEST_DATABASE_URL`が渡される場合は既存PostgreSQL serviceを再利用します。

文書アップロードでは`mimetype` v1.4.15の内容判定を使い、テキストと確認したデータに限ってCSV・EML・TXT拡張子をfallbackとして受け付けます。メール解析は`go-message` v0.18.2へ移し、`html-to-markdown/v2` v2.5.2のtable pluginでHTML注文メールの表を保ちます。最大文書サイズ、MIME深度、part数、添付警告、plain text優先を維持しました。`web`では`vitest` v5.0.3と`fast-check` v4.10.2を導入し、Ledger URL検索条件のparse/serialize往復不変条件を500回検査します。GraphQLのMIME・HTTP、メール解析、Worker/API injector、GraphQL DB統合、property testの対象テストは成功しました。

`chi`はEchoと重複するため、Bob Query GenerationはSQL文字列を入力として生SQLを直接書かない方針と競合するため採用しません。DataLoaderや`retry-go`は既存のBob一括取得とDB永続job retryに対する追加効果を確認できません。`testify`の一括導入は標準`testing`の明示的な失敗理由を失う置換が多いため見送りました。

### 2026年10月14日の冪等性と評価

資金記録、送金、購入確定では家庭行をロックした後に冪等キーを再読込し、同時に届いた同一要求が先行transactionのリソースを返すようにしました。Testcontainersを使うGraphQL DB統合テストで、同じキーの並行要求が同じリソースIDを返し、仕訳と台帳行が各1件だけ作られることを確認しました。オフライン評価器には`ExtractionRun.Receipt`ごとのモデル別精度、推定費用、記録された遅延の比較を追加しました。合成fixtureのみで評価し、Gemini APIは呼び出していません。対象のGoテストと`mise run evaluation:run`が成功しました。

購入画面の`convertPurchaseItems`をpage model内の純粋関数として分離しました。`fast-check`の300反復で、1〜30件の任意品目について金額合計の保持と正の配賦weightだけを送る性質を確認します。Vitest、Frontend型検査、lint、FSD、format検査が成功しました。

`fake-indexeddb` v6.2.5を使った実IndexedDBテストを追加しました。Outboxの同一entryを並行claimしても一方のownerだけが取得し、失敗後の再送でも元の冪等キーを保ち、成功後にentryが削除されることを1つの経路で確認します。Frontendの対象test、型検査、FSD、format検査が成功したため、Outbox処理のテスト不足をレビューから削除しました。

### 2026年10月14日の公開URL遷移確認

公開環境の合成検証用家庭で`imports`から`settlement`へ移動し、URL更新、`settlement`の直接再読み込み、ブラウザーの戻る・進むでrouteと画面が一致することを確認しました。未保存入力中の離脱確認と他routeは未確認のため、UXレビューの該当項目は検証範囲を限定して残しました。

2026年10月14日に公開環境の合成家庭で帳簿・精算・分析・取り込み・設定のURLを直接開き、それぞれ再読み込みしてもURLと画面見出しが一致することを確認しました。公開環境のroute再読み込みに関する未解決項目は解決済みとしてレビューから削除しました。未保存入力中の離脱確認は未解決です。

### 2026年10月14日のApplication入力検証

`validator/v10`を再利用する`inputvalidation.Check`をApplicationへ追加し、タグ・カテゴリ・商品別名・予算・定期支出の文字列空欄と最大長を検証します。今回、商品概念と商品規格の名称にも同じ検証を適用しました。商品名はDBの文字数上限と一致する240文字を受け付け、空のJANコードは拒否します。GraphQLの必須値、scalar、Global ID形式はgqlgenに任せ、金額・期間・配賦・会計整合性は各Serviceに残します。導入時に追加した境界検証テストと対象ApplicationのGoテスト、`mise run lint`が成功しています。

### Gemini利用予算の制御

`docs/reviews/all2.md`が提案するアプリ内の月間利用予算・家庭別上限は追加しません。Google Cloud側のSpend Capを500円に設定し、Spend Capが利用可能な状態であることを確認済みです。費用制限と通知をGoogle Cloudで管理し、同じ役割の予算予約や利用集計をアプリへ重複実装しない判断です。Spend Capの状態や課金上限機能が利用できなくなった場合は再検討します。

### 明細取込の確定状態

カード明細CSVの各行にサーバーが`row:N`形式の`sourceReference`を付け、照合結果と確定Mutationで同じ値を使います。取引番号が重複するCSVでも行ごとの識別を維持します。確認画面は選択中の文書から永続化済みの`document_confirmations`を読み込み、成功後の確認状態と合わせて再読み込み後も追加済み行を表示します。関連するGo統合テストとImportsのVitest 9件が成功し、修正済み項目を`docs/reviews/all2.md`から削除しました。

### 2026年10月14日のカード引落しと外部入金の明細確認

銀行明細の`CARD_BILL_PAYMENT`は既存の口座移動Mutationを使い、銀行口座からカード口座への記帳として確定します。`EXTERNAL_INCOME`は明細行の正額、日時、銀行口座を既存の外部入金Mutationへ渡します。サーバーでも明細連携時の入金先を銀行口座に限定し、現金口座を指定したGraphQL呼び出しを拒否します。どちらも最新の成功済み抽出結果をサーバーで再読込し、明細確認、会計記録、仕訳、冪等キーを同じDB transactionで保存します。`document_confirmations`の複合主キーで同じ文書行の二重記帳を拒否します。既存のTestcontainers検証では成功、同じキーの再送、別キー重複、金額・日時不一致、均衡仕訳を確認し、今回現金口座拒否を同じ統合テストへ追加しました。Importsの対象Vitest、型検査、Steiger、Relay検証、lintも成功しています。異なる文書に記載された同一取引の重複照合は未対応です。

### 2026年10月14日の貸付・返済明細

銀行明細の`LOAN`と`REPAYMENT`を既存の貸付・返済Mutationへ接続しました。サーバーは世帯内の銀行原本と最新の成功済み抽出結果から明細行を再取得し、種別、符号付き金額、日時、選択口座を照合してから記帳します。`document_confirmations`へtransferを紐付け、明細行確認と貸付・返済、仕訳を同じtransactionで保存します。Testcontainers統合テストで同一キー再送、別キー重複、金額・日時・口座・種別・世帯の不一致と返済記録を確認しました。

### 2026年10月14日の異文書間の取引照合

購入証拠に加え、銀行明細の貸付・返済行から種別、金額、日時が合う既存取引を候補として提示し、利用者の選択後に銀行口座と仕訳方向をtransaction内で再検証して紐付けます。候補を自動選択せず、取引を新規作成せずにconfirmationだけを保存することで、反復する同額取引の自動統合を避けます。候補一覧はRelay cursorで100件ずつ取得します。口座間振替、カード引落し、返金、外部入金の確定済み記録に対する異文書間リンクは未対応です。

### 2026年10月14日の画面固有CSS

画面だけで使うCSSを各page sliceへ移し、`global.css`には複数画面で共有する規則とapp層の規則を残しました。残存selectorを画面間で監査し、ページ専用の規則が残っていないことを確認しました。画面固有CSSの指摘を`docs/reviews/simple.md`から削除しました。

### オフライン購入データの利用者分離

購入下書きとOutboxの保存キーに`memberId`と`householdId`を含め、enqueue、claim、再送、完了、失敗、破棄で同じscopeを検証します。送信時は現在の利用者が対象家庭に所属することを確認し、ログイン利用者が変わった直後に前利用者のOutboxを表示しません。所有者が記録されていない旧v2データは削除も引継ぎもせず、旧storeに保持して送信対象から除外します。IndexedDBテスト3件とFrontend型検査が成功したため、該当指摘を`docs/reviews/all2.md`から削除しました。

### 返品、購入日、PDF原本照合

カード明細の負額は`Math.abs`で正額へ変換せず、`REFUND`行を利用者が選択した確定済み購入または品目への訂正として記録します。最新の成功した抽出結果から返金額、原本領域、観測内容を取得し、訂正・明細確定・証拠を同一transactionへ保存します。同じ明細行の二重確定と残額を超える返金を拒否し、負額の`CARD_PURCHASE`も返金として扱いません。Testcontainers PostgreSQL 18の`TestCorrectPurchaseFromStatementKeepsRowEvidenceAtomic`で冪等再試行、証拠保持、重複拒否、過剰返金のrollback、世帯分離を確認しました。購入日の抽出値がないレシートは、利用者が日付を入力するまで確定できません。PDFは最大20ページ、ページあたり400万画素、合計4000万画素までレンダリングして独立OCRとGeminiの候補を照合し、上限超過やOCR失敗時は`Balanced=false`にします。Go側の130秒request timeoutでworkerは失敗として戻りますが、PaddleOCRの同期推論はPythonプロセス内から強制停止できないため、timeout後にOCR serviceの処理が続く可能性は残ります。関連Importsテスト9件、OCR/Workerテスト、OCR serviceのunittest 6件、FSD検査とFrontend型検査が成功したため、解決した項目を`docs/reviews/all2.md`から削除しました。

### 2026年10月14日の複数明細抽出とメール添付

カード・銀行明細のPDF・画像をsourceKind別のGemini schemaで`transactions[]`として抽出します。Go側で符号付き金額、RFC3339日時、取引種別、各行の原本領域を検証し、独立OCRと利用先・金額・未抽出行候補が一致しない場合は未確定にします。メール添付のPDF・JPEG・PNG・WebPは親メールとは別のsource documentとjobにし、家庭境界付きの親子relationを親jobと同じDB transactionへ保存します。DB登録後にqueueへ通知し、同一jobの競合登録では保存済みjob IDを返します。添付のみのメール本文は親jobでGeminiを呼ばず、添付ごとの解析を継続します。添付文書の再解析画面からカード・銀行明細を選択でき、確認・証拠登録済み文書では再分類操作を表示しません。APIは文書一覧の`hasEvidence`をBob DSLでまとめて取得します。TestcontainersのPostgreSQL 18によるAPI統合テスト、`go test ./internal/ingestion`、Frontend対象テスト、型検査、FSD検査、`git diff --check`が成功しました。sourceKind選択UIの指摘を`docs/reviews/all2.md`から削除しました。

### 2026年10月14日のGemini無料枠再試行

無料Projectで429が返った場合に有料Projectへ即時に切り替えず、Gemini APIの`Retry-After`をWorkerの既存job再試行へ渡します。429では有料Clientが呼ばれないこと、一時的な503では従来どおり有料へフォールバックすること、再試行遅延が保たれることを`go test ./internal/ingestion`で確認しました。関連する429の指摘を`docs/reviews/gemini.md`から削除しました。

品質再検証は初回抽出の成功Projectを引き継がず、無料Projectだけを使います。初回抽出が有料へフォールバックした後に品質再検証の無料APIが429または一時的な5xxを返した場合、Workerは再検証結果を採用せず初回結果を維持し、品質再検証からPaidへのfallbackを行いません。税額や合計の重大な不整合に対する既存のPro昇格条件は維持します。Project routeが有料の前回結果から品質再検証を開始し、無料429・5xx後に品質再検証がPaid APIを呼ばないことをRouterテストで確認しました。解消した指摘を`docs/reviews/gemini.md`から削除しました。

email/order_emailの無料Liteモデルが429を返した場合だけ、同一無料ProjectのFlashモデルを試します。無料Flashの429ではRetry-Afterを維持してPaidへ切り替えず、一時的な5xxでは既存のfallback方針に従い有料Flashを試します。レシートのFlashからLiteへのfallbackは行いません。Router統合テストで無料Flash成功、429後の再試行、有料Flash成功の各経路について、呼出し数、モデル、Project route、Reason、retry delay、費用記録を確認しました。残るモデル横断routingの制限を`docs/reviews/gemini.md`に記載しました。

### 2026年10月14日のGemini無効時のjob claim

Geminiを無効にしたWorkerは、対応するカード・銀行CSV文書に結び付いたjobだけをBob DSLのサブクエリでclaimします。Geminiが必要なjobを順に`FOR UPDATE SKIP LOCKED`し、除外IDを`NOT IN`へ積み上げる処理を削除しました。ingestionの`TestMain`はDB接続URLが未指定の場合にTestcontainersでPostgreSQL 18を起動してmigrationを適用します。CIと同じDB統合テストをローカルでも実行でき、Gemini無効時の候補選択とSpend Cap再queueを確認しました。通常の無料429では有料へ移行しないため、Spend Cap経路のfixtureは無料APIの一時的な503から有料APIへ進む設定に直しました。`go test ./internal/ingestion`とgolangci-lintは成功しました。実データ量での性能測定は行っていません。該当する検索方法の指摘を`docs/reviews/all2.md`から削除し、負荷が増えた場合はQueryの実行計画とWorkerの処理時間を測定して再評価します。

### Gemini失敗呼び出しの利用量記録

MAX_TOKENSや不正生成JSONでGemini呼び出しに失敗した場合も、応答にUsageMetadataがあれば入力・出力token数と有料ルートの推定費用を記録します。runの失敗statusとerror codeは維持します。UsageMetadata付きMAX_TOKENSと不正JSONを返すテストでtoken数、推定費用、失敗状態を確認しました。

### メール添付の明細種別と予算一覧

`POST /documents/{id}/retry`は同一householdの未確定メール添付に限り、sourceKindをカード・銀行明細へ変更して再解析できます。sourceKind変更とjob登録は同一transactionで処理し、queued jobでは既存IDを再通知します。running、親メール、非添付、確認・証拠登録済み文書の再分類は拒否します。予算一覧は対象月の購入、品目、訂正をBob DSLで一括取得し、全体額・カテゴリ別額を集計することで予算・購入ごとのN+1を解消しました。統合テストで処理結果とメンバー認可を確認しました。

### 2026年10月14日のUI再レビュー対応

モバイルドロワーをBase UI Dialogで実装し、フォーカス移動・復帰、Escape、背景操作制限を有効にしました。追加ボタンのアイコン色を白にし、家庭切り替えボタンは家庭が複数ある場合だけ操作可能にしました。ホームのSetupGuidanceを概要より前に移し、ホームCSSの重複定義を統合しました。添付親子の選択時に一覧外の文書を単独取得し、フィルターやページネーションをまたいでレビューを続けられるようにしました。Frontend型検査、Vitest対象5ファイル17件、markuplint、stylelint、oxfmt、FSD検査が成功しました。具体的な修正対象がなくなったため`docs/reviews/design2.md`を削除しました。

### Gemini SchemaとGo型の契約

`receiptSchema()`と`statementSchema()`はGoの出力型と別に定義しています。`TestStatementSchemaMatchesExtractionModel`を追加し、Schemaのfield、JSON型、必須項目と座標範囲を`StatementEntry`に照合しました。ReceiptとStatementの双方にSchema契約テストがあるため、二重定義そのものを生成方式へ移す指摘を`docs/reviews/be-architecture.md`から削除しました。Schema変更時は該当テストを更新します。

取り込みフィルターの適用条件をTanStack RouterのURL検索値へ集約しました。ブラウザの戻る・進むで条件が復元され、未適用の入力値は現在のURL位置に結び付けて保持します。Frontend型検査、対象Vitest、oxlint、Steigerが成功しました。家庭メンバー管理には、OWNERロールを既存メンバーへ移譲するMutationと確認操作を追加しました。ロール変更と監査イベントを同一transactionで保存し、移譲後の権限、本人移譲の拒否、再移譲をTestcontainers統合テストで確認しました。

### 2026年10月14日の部分返品とメール添付レビュー

締め済み月に発生した5,000円の購入から2,000円を部分返品するTestcontainers統合テストを追加しました。確定済み期間を変更せず、返品額の仕訳と口座残高、購入月の支出、翌月精算への繰越しが一致することを確認しました。カード明細の`REFUND`行は選択した購入または品目への訂正として記録し、明細の原本領域と抽出内容を保持します。`TestPostedPurchaseCorrectionAffectsNextSettlementWithoutRewritingClosedPeriod`と`TestCorrectPurchaseFromStatementKeepsRowEvidenceAtomic`をTestcontainers PostgreSQL 18で実行し、締め後の訂正と明細返金の冪等性・原子性を確認しました。メール本文が短い場合と添付のみの場合は既存のMIME解析・Worker処理で保持され、添付文書を独立した解析jobとして処理します。既存のingestionテストで検証済みです。返金の指摘は解決しましたが、他の明細取引種別は`docs/reviews/lack.md`に未解決項目として残しています。

### 2026年10月14日の未保存画面と取り込みURL

世帯内の画面はTanStack Startのfile-based routingでpathを切り替え、取り込みフィルターはTanStack Routerのsearch stateから読み書きします。未保存状態は世帯とページをscopeとするregistryに集約し、画面遷移は`useBlocker`、ブラウザー離脱は`enableBeforeUnload`で確認します。モバイル幅では`html`とdrawerに`overscroll-behavior-y: none`を設定し、drawer背景はBase UI Dialogのmodal scroll lockで固定します。registryはページ内の複数フォームを個別登録しないため、共有callbackを使う画面は各フォームの状態を内部で集約します。route contextの対象test、型検査、対象oxlint、Steiger、format検査が成功しました。ブラウザー更新・タブ終了時の離脱確認は未解決です。

### 2026年10月14日の購入メール転送

購入メールの取り込みには、世帯ごとに発行・失効できるHorse専用転送先を使います。転送先tokenのhashだけをDBへ保存し、平文のtokenと完全な転送先アドレスは発行・交換時にだけ返します。再表示できないため、転送先を失った場合は交換します。IMAPS接続はread-onlyで行い、mailboxの既読状態やメッセージを変更しません。mailboxの`UIDVALIDITY`と`UID`をDBへ保存し、原本・添付文書・抽出jobの登録とcursor更新を同一DB transactionで確定します。SeaweedFSへの原本・添付保存はDB transaction外の操作なので、DB rollback時に孤立objectが残る可能性があります。メール本文はアプリケーション側でAES-GCM暗号化して保存し、本文のSHA-256ではなく暗号化用keyによるHMACをobject識別値として使います。workerは新旧key ID付き暗号形式を読み取ります。

転送先の検証では、`Delivered-To`が1件だけ存在し、設定済みの専用mailbox base addressに世帯tokenのtagを付けた形式であることを要求します。本文の`To`は転送時に欠落・変更され得るうえ、送信者が指定できるため、宛先の認証には使いません。復号したtokenからhashを計算し、有効な転送先だけを世帯へ対応付けます。`UIDVALIDITY`が変わったときはcursorを再初期化して再走査し、文書hashの一意制約で重複登録を防ぎます。合成メールによるStalwartの`Delivered-To`とplus-tag保持、および実環境でのIMAPS経路は未確認です。IMAPS資格情報の設定、メール本文・原本のretentionと削除も未完了です。これらのレビュー項目は検証・実装が終わるまで未解決のまま残します。

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

変更は`web/src/app/horse-app.tsx`と画面ごとの編集フォームにあります。公開環境では履歴操作と深いURLからの再読み込みを確認しました。iOS実機でのpull-to-refresh抑止と、タブを閉じる・更新する場合の未保存確認は未確認のため、`docs/reviews/spa.md`に残します。

ソース監査でdirty状態に対する`useBlocker`と`beforeunload`、購入下書きのIndexedDB保存、drawer表示中の`overscroll-behavior`とbody固定を確認しました。2026年10月13日に公開環境で帳簿への遷移によるURL変更、深いURLの直接表示、再読み込み後の画面維持、戻る・進むを確認しました。dirtyな購入入力からホームへ移動すると破棄確認が表示されました。別の合成入力では直接再読み込み後も品名と金額が下書きから復元されましたが、ブラウザー確認は表示されませんでした。合成入力は空に戻し、購入は送信していません。タブを閉じた後の復元、下書き保存前の離脱、Android/iOS実機でのpull-to-refresh抑止は未確認のため、`docs/reviews/spa.md`に残します。

UXレビューの品目では、商品サジェストを選択した直後に自由入力用handlerが消していたvariant IDを、名前変更後に設定するよう順序を直しました。取込画面では、編集中のレシート結果をポーリング応答で上書きしないようにし、明示的な再抽出・購入確定後に保護を解除します。精算画面では世帯と期間を応答snapshotに紐付け、古い応答を破棄し、締めMutationには表示中snapshotのrevisionを使います。各変更の型検査と整形確認は成功し、総合CIは後続のmain実行で確認します。実操作を伴うフロントエンドのtest runnerがなく、小規模なstate更新順序やrequest世代管理だけを検証するモック中心のテストは追加しませんでした。対応した3項目を`docs/reviews/ux.md`から削除し、未解決項目は残しています。

### Dockerと公開イメージ

Go API・Worker・Atlas MigrationとWebを別イメージに分け、依存取得のBuildKit cache mount、main push時のイメージ公開、provenance、SBOMを設定しました。CI run `38392241924`でGo、Web、OCRイメージの公開に成功しました。OCRのソースは変更していないためinfraのOCR digestは維持しました。infra commit `664ff46b3`でAPI・Worker・WebのdigestとWebの正しい`horse-web`リポジトリを反映し、Argo CDが`Synced`・`Healthy`になり4 PodがReadyであることを確認しました。Web実行イメージをNode 24 distrolessへ切り替えたあと、Kubernetes側のcommand指定で起動できないことが分かりました。infra commit `92433d001`でcommand指定を削除し、Argo CDが`Synced`・`Healthy`、4 PodがReady、公開URLがHTTP 200であることを確認したため、Dockerの未解決レビュー項目を削除しました。

Renovateが更新できるよう、Go・Web・OCRの公開タグはGitHub Actionsのrun numberをpatch番号にした`v0.0.<run number>`へ統一します。SHAを含む疑似バージョンは使いません。infra commit `16c224ca8`で既存digestに厳密なタグを付け、Webの起動commandがNode distroless imageと合わない問題をinfra commit `92433d001`で修正しました。Horseの次のCIでroute変更を含む新しいdigestを公開し、Argo CDと公開URLの稼働を確認してから、`horse`、`horse-web`、`horse-ocr`の過去のpackage versionsを削除します。稼働中indexが参照するplatform manifestとprovenance/SBOM manifestは削除対象にしません。

### ファイルベースルーティングとURL状態

ページ名を`?page=`へ保存する方式を廃止し、TanStack Startのファイルベースルートで`/households/$householdId/{page}`を構成します。帳簿の検索条件は共有と再読み込みに必要なためsearch paramsへ保存します。画面別ルートは`pages/`のpublic APIを呼び出し、App shellをroot routeに一度だけ配置して未保存変更のblockerと画面状態を維持します。家計取得に失敗して画面が空の場合は、表示中の家計を選び直す操作で取得を再試行します。型検査、Steiger、oxlint、oxfmt、Vite buildを確認しました。新しいimageの稼働確認と直接URL・戻る/進むの公開環境確認はCIとデプロイ後に記録します。

### 追加レビュー対応

GraphQL読取のエラー境界は、ページ遷移で状態をリセットし、再試行時に失敗した画面だけを再マウントするようにしました。共通エラー文言と再試行ボタンを用意し、フルページの再読み込みを使わない構成です。Frontend型検査と整形を確認しました。

会計期間を締める操作に、対象月、全体額、メンバー別金額を確認する画面を追加しました。確認チェックを選択するまで確定ボタンを無効にし、期間変更で確認状態を解除します。Frontend型検査、CSS検査、整形を確認しました。

CSV明細の永続参照は取引番号から行番号へ変更しました。TransactionIDは照合属性に保ち、同一取引番号の複数行を別々に確定・冪等再試行する統合テストを追加しました。購入訂正は品目別と購入全体の両方の残額を検証し、全体返金後に追加返金できない統合テストを追加しました。BobのDSLで関連データを読み、合計値のoverflowも拒否します。非DBの取込照合テストは成功しました。DB統合テストは`HORSE_TEST_DATABASE_URL`未設定のためローカルでは実行しておらず、GitHub Actionsで実行します。

取り込み画面は最大3ファイルを並列送信し、個別失敗をキューに残して再送できるようにしました。認識完了を待って次の原本を送る直列処理を廃止しました。原本のファイル名はDBへ保存し、アップロード応答・一覧・詳細APIで返します。店舗・取り込み日・原本種別・確定状態による絞り込みを追加し、カーソルを世帯と全条件へ紐付けました。店舗検索はレシートと明細CSVの両方に対応します。Go APIテストとFrontend型検査が成功しました。

購入訂正の精算調整は購入時点の支払元ルール、プール負担スナップショット、資金拠出ルールから再計算します。後続のルール変更後に返金訂正しても過去の設定を使うDB統合テストを追加しました。購入全体と品目の両方で返金上限を検証します。Goテストとlintを確認しました。DB統合テストは`HORSE_TEST_DATABASE_URL`未設定のためローカルでは実行していません。

### GraphQL Schema

Mutation単位とドメイン型単位でSDLを分割し、gqlgenは`follow-schema`を使います。全37 Mutationが`Success | UserError`のResult unionを返し、共通`MutationError` interfaceを使います。Ledger、Settlement、Pool、Observation、PurchaseCorrection、AccountBalanceSourceも型の違いをinterface/unionで表しています。この構成はpiccaのADR 0002とSDL分割に沿うため、該当するレビュー指摘は解決済みとして`docs/reviews/graphql.md`から削除しました。根拠は`gqlgen.yml`、`graphql/*_mutation.graphql`、各domain SDLです。

nullable outputの静的監査では、明白にnon-nullへ変更できるfieldは見つかりませんでした。任意の関連ID・終了日・商品数量・OCRの観測日時など、nullableが値の欠損条件を表す箇所は維持します。一方、default付きの`includeArchived`と`productSuggestions.first`はSDLでnon-nullにできる候補ですが、Relay operationとの互換性確認が必要です。`ProductVariant.brand`と`packageSize`はnullと空文字の扱いがresolver間で揃っていません。Relayを使わずread関数が全ページを順次取得する一覧と、nullableの残課題を`docs/reviews/graphql.md`に残します。run `38392241924`ではgqlgenとRelay生成物、Go DB統合テスト、Frontend型・FSD・lint・format・buildが成功しました。

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

取り込み一覧に古い履歴の追加取得を実装し、ポーリング後も取得済みページを保持します。検索条件と原本ファイル名の保存は未実装のため、`docs/reviews/ux.md`に残します。

仕訳明細の一括登録をBobの複数行INSERTへ変更しました。`go test ./internal/application/accounting`が成功しました。

初回表示時にsearch paramsを正規化する処理が子ルートをホームへ戻すため削除しました。公開環境で深いURLの再読み込み、戻る・進むを確認する必要があります。

### 2026年10月13日のレビュー整理

`docs/reviews/all.md`の購入全体訂正における負担比率比較は、入力比率と保存済み金額比率を最大公約数で正規化して比較するよう変更しました。異なる比率、異なるプール、重複プール、不正な保存値を拒否する単体テストを追加し、`internal/graph`のテストとlintが成功したため、該当項目をレビューから削除しました。

Geminiの未知モデル価格は`unknown_model_price`として明示し、有料ルートで価格未登録モデルを呼び出す前に拒否するようにしました。無料ルートの試行は維持し、未知モデルの拒否と費用メタデータを検査するテストを追加しました。`internal/ingestion`のテストとlintが成功したため、該当項目をレビューから削除しました。

`docs/reviews/be-architecture2.md`のUUID変換とApplication共通エラー分類は、入力UUIDのparse errorを`ErrBadInput`として返し、共通の`apperror`分類をGraphQL結果へ変換する実装に整理しました。購入作成・確定・更新の不正IDと共通エラー分類を検証する単体テストを追加し、対象Goパッケージのテストとlintが成功したため、同文書の「型とエラーの扱い」節を削除しました。

`docs/reviews/ux.md`では、取り込み確定時の複数決済・品目カテゴリ・商品variant選択、モバイル文字サイズ、タップ領域、購入入力順序、通知種別、原本と編集欄の配置、TanStack Routerの`Link`による画面遷移、初回設定ガイドを実装済みとして整理しました。商品variantは`productSuggestions`で候補を検索し、選択IDを購入作成へ渡します。初回設定ガイドは既存の家庭データと購入件数から未設定項目を判定し、負担先・決済元・標準配分の該当sectionへ移動します。確認した箇所は`web/src/pages/imports/ui/imports-page.tsx`、`web/src/pages/purchase-create/ui/purchase-create-page.tsx`、`web/src/pages/home/ui/home-page.tsx`、`web/src/pages/home/ui/setup-guidance.tsx`、`web/src/app/horse-app.tsx`、`web/src/app/styles/global.css`、`web/src/shared/ui/notice.tsx`です。Frontendの型検査、FSD、lint、formatが成功しました。ホームの7日間支出グラフには日付と金額の代替ラベルを追加しました。

購入下書きは、編集後に古い`savedAt`が残っていても「未保存」と表示するよう変更しました。保存開始・成功・失敗を区別する状態表示と保存失敗時の再試行は未実装です。撮影入力と複数ファイル入力は分離しましたが、撮影後のプレビュー・回転・トリミングと実機のカメラ操作は未確認です。モバイルのpull-to-refreshと未保存状態でのタブ終了も実機・ブラウザー検証が残っています。

購入訂正の過去ルールsnapshot、原本照合の直近100件取得、負担ルール一覧のバッチ読込はDB統合テストを含む変更後のGitHub Actions成功を確認できていないため、`docs/reviews/all.md`に残しました。原本照合の統合テストはrun `38412209424`でBob DSLの定数型不一致を検出しました。定数をtext型に直し、最新migrationを適用した一時DBで対象テストが成功しました。次のGitHub Actionsで再確認します。商品候補検索の実データ性能計測、帳簿のサーバー側検索、設定画面の情報設計、不確実な項目の順次確認、エラー通知からの操作、コントラストは未完了として残しています。

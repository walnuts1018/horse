# 完成要件と検証

`plan.md`の機能と運用要件を満たし、kurumi上の`https://horse.walnuts.dev`で実際に利用できる状態を完成条件とする。
実装ファイルやマニフェストが存在するだけでは完了と判定せず、各要件に対応する動作の証拠を確認する。

## 要件別の証拠

| 要件 | 完成を確認する証拠 | 現在の検証状態 |
| --- | --- | --- |
| 複数家庭、所属と権限 | 別家庭の全API・原本アクセス拒否と招待処理の統合テスト | GraphQL・原本・証拠の所属検証、招待の発行・受諾・失効・期限・再利用拒否のDB統合テストを確認。公開環境ではOIDCログイン後の家庭作成と再表示を確認 |
| 複数のプール、実口座、決済手段 | 個人・共用プールと口座、複数決済を含む購入の統合テスト | 公開環境の合成家庭で銀行50円・現金51円の複数決済による101円の購入を登録し、再読み込み後に両決済額、合計、品目、共用プール配賦が残ることを確認 |
| 負担と決済の分離 | 共用口座で個人購入、個人カードで共用購入の台帳と精算の期待値 | 精算の共有費調整・個人口座の立替控除を純粋関数で検証。共有口座の個人支出はDB統合テストで確認。`TestPersonalCardPaysSharedPoolPurchase`で個人カードによる共用購入の残高、貸借一致、60対40の精算額を最新migration適用済みの一時DBで確認 |
| 整数円と端数保存 | 最大剰余配賦、同率端数、整数上限、合計保存のテスト | 取り込み配賦と会計処理のテストを確認。全会計経路を対象にした自動テストは未完了 |
| 負担ルールの優先順位 | 品目・商品・カテゴリ・店舗・全体ルールと競合条件のテスト | 品目・商品バリアント・商品概念・カテゴリ・店舗・全体ルールの優先順位とスナップショットのDB統合テスト通過 |
| 過去のスナップショット | デフォルト更新後の確定済み品目・締め済み精算が不変である統合テスト | 品目の負担ルールと締め済み精算の資金拠出設定のスナップショット保持をDB統合テストで確認 |
| 予定入金と実入金 | 未入金、一部入金、過払い、イレギュラー入金、繰越しの統合テスト | 予定と実入金の分離、未入金額の精算反映、締め後の拠出設定スナップショットと繰越しをDB統合テストで確認。`TestIrregularFundingOverpaymentCarriesAcrossClosedPeriod`でAの予定6,000円に対する実入金10,000円とBの未入金を精算し、Aの過払い4,000円とBの未入金4,000円を翌月へ繰り越すことを確認 |
| 精算 | 入金控除、共用資金の個人利用、直接送金、負額の繰越し、内訳への遷移 | 共有費調整、個人口座による立替控除、締め残高の翌月繰越しをテストで確認。複数月previewと月ごとの精算結果の一致を`TestSettlementPreviewMatchesMonthlyCalculationsAfterClosedCarry`で確認し、CI run `38392241924`が成功 |
| 貸付・返済・贈与 | 支出に含まれないこと、返済の債務対応、共用口座経由の返済 | DB統合テストで貸付・贈与の記録、共用口座経由の返済による貸付残高の解消、口座残高・仕訳・支出集計への反映を確認 |
| 締めと訂正 | 締め後編集の拒否、元履歴保持、訂正仕訳、次回精算と購買日分析への反映 | `TestPostedPurchaseCorrectionAffectsNextSettlementWithoutRewritingClosedPeriod`で5,000円の購入から2,000円を部分返品し、締め済み期間を変更せず、返品仕訳・口座残高・購入月の負担と支出・翌月精算の一致をTestcontainers PostgreSQL 18で確認。以前のCI run `38392241924`も成功 |
| 購入登録と記帳 | 通常登録・レシート取込・カード明細取込を一つのMutationで確定し、購入・仕訳・原本証拠を原子的に保存 | Frontendの通常登録とImportsのreceipt・statement確定は`createAndPostPurchase`を呼び、購入作成と記帳をサーバーの同一トランザクションで実行する。レシートの品目と抽出観測metadata、カード明細の`sourceReference`、DocumentConfirmation、EvidenceLinkを同じトランザクションに保存する。カード明細の`REFUND`は選択した確定済み購入または品目への訂正として記録し、DocumentConfirmationと原本領域・抽出内容を同一transactionに保存する。`TestCorrectPurchaseFromStatementKeepsRowEvidenceAtomic`は冪等再試行、重複行の拒否、過剰返金時のrollback、負額のCARD_PURCHASE拒否、世帯分離をTestcontainers PostgreSQL 18で確認する。`TestCreateAndPostPurchaseIsAtomicAndIdempotent`は記帳・冪等再試行・レシート品目証拠と失敗時のrollbackを検証する。 |
| 台帳不変条件 | DBでの貸借一致、品目配賦合計一致、二重精算の防止、トランザクションの統合テスト | 会計統合テストの対象世帯について、Bobクエリで全仕訳明細を取得し、仕訳単位の貸借一致と明細存在を検証する共通関数を、購入記帳・訂正・予定口座移動・銀行明細の口座移動・外部入金・返金・貸付返済の統合テストで実行し、各品目の配賦合計と金額の一致も検証。配賦・更新の原子性、購入登録の冪等性も確認。別会計月の返済2件が同時に到着しても元本を超えないことを専用DBで20回確認。`TestServiceUpdatePersistsRevisionEventAndRejectsClosedPeriod`で購入更新のrevision・event保存と締め済み期間の更新拒否を確認。未実行の仕訳経路を含む全経路の自動テストと二重精算防止の検証は未完了 |
| モバイル幅の連続入力 | 390px幅での商品名・金額・次への連続入力 | ローカル390×844で3品目の保存・再表示を確認 |
| PCのグリッド入力 | Tab・Enter・矢印・Undo、複数行貼付け、購入共通項目の継承 | 2026年10月12日21:58 UTCに公開環境の「公開環境の動作検証」家庭で、PCブラウザーから合成品目3行を貼り付け、702円・3品目の表示を確認。「元に戻す」で追加3行がまとめて消え、0品目へ戻ることを確認。品目行のEnterと矢印で隣接セル、同一列の次行、追加入力欄へ移動する実装を追加。保存前の下書き操作でDBへは登録していない。手動カテゴリは複数行貼付けにも適用し、未選択時は標準カテゴリを使う |
| 下書きとオフライン | IndexedDB復元、再接続、冪等再送、同期・未送信・競合の表示 | API停止時の送信待ち保持、再読み込み後の復元と再送を確認。下書き・Outboxを`memberId + householdId`で分離し、送信時に家庭所属を再確認する。所有者不明の旧v2データは保持して一覧・送信対象から除外する。IndexedDBのscope・旧データ保持を扱うテスト3件とFrontend型検査が成功。 |
| 一括編集 | カテゴリ・負担・店舗・決済元・日付・タグ・正規化先の一括変更と一括Undo | カテゴリ・負担・タグ・正規化先に加え、店舗・決済元・日付もrevision検査付きの一括Mutationで更新し、競合時は全件をrollbackする。画面からの一括Undoも単一Mutationにまとめた。DB統合テストで下書き購入の異なるフィールドの統合と競合時のrollback、POSTED購入の決済行と仕訳同期を確認 |
| サジェストとコピー | 店名・商品・カテゴリ・決済手段の候補、前回購入コピー、価格の未確定扱い | 商品候補・履歴補完・前回購入コピーを実装済み。カテゴリ推定は手動選択を維持し、別品目には標準カテゴリを使うよう修正済み |
| 入金・口座移動履歴 | Relay cursorによる継続読込、件数とページ情報の一貫性 | AccountIncomeとAccountMovementをforward connectionへ変更し、画面の追加読込を実装済み。CI run `38392241924`のGo統合テストとFrontend検査が成功し、infra commit `664ff46b3`で公開環境へ配備済み。 |
| 同時編集 | 異なるフィールドの統合、同じフィールドの競合提示、金額と配賦の原子性、カーソル再同期 | CI run `38392241924`のGo DB統合テストで同一会計月内の異なるフィールド更新、同じフィールドの競合、締め後訂正、口座移動の残高分析、口座残高分析Connectionの複数年ページングを確認。購入日時の会計月が変わる同時更新は安全のため競合扱い。 |
| 原本・観測・証拠 | S3非公開原本保存、家庭別認可、品目と矩形領域の双方向対応、補正行列 | ローカル原本保存・家庭別認可、観測と座標の保存テストを確認。JPEG・PNG・WebP・PDFのページプレビュー、補正行列を使った品目/OCR領域のオーバーレイと選択、ページ指定、原本座標によるクロップ再解析UIを実装済み。Importsの一覧・詳細・プレビューはTanStack Queryで取得し、Blob URLは表示中だけ生成する。 |
| AI画像解析と独立OCR | 原本を直接渡したGemini解析、PP-OCRv6との不一致表示、難所のクロップ・再解析 | PP-OCRv6のコンテナ推論経路は合成英語画像で確認。合成画像を使いFree/PaidのGemini実呼び出しを各1回実施し、両方で品目264円・242円、合計506円を抽出。Verifyは`Balanced=false`で、税区分の整合を確認できなかった。2026年10月14日に隔離した検証用JobからPaid Projectへ合成PNGを1回送り、抽出結果を確認した。一時Job、Pod、ServiceAccount、NetworkPolicyは削除済みで、通常Workerの`GEMINI_ENABLED=true`とPod Readyを確認。 |
| 税・値引き・支払手段 | 混在税率、グループ端数、曖昧性、返品、ポイントと値引きの区別の自動テスト | 基本計算テスト通過。カード・銀行明細のPDF・画像はsourceKind別の複数取引抽出、符号・日付・行領域を独立OCRと照合し、不一致を未確定にする。カード引落し、外部入金、貸付、返済は金額・日付・口座・明細行を照合し、仕訳とconfirmationを同一transactionで保存する。Testcontainers統合テストで再送、別key重複、金額・日付・口座不一致を確認。貸付・返済は異なる銀行明細文書の行を既存記録へ手動紐付けでき、Testcontainersで取引の二重作成防止と不一致拒否を確認。メール添付のsourceKind再分類APIと種別選択UIを実装済み。 |
| 複数ソース照合 | 同原本重複、注文・分割発送・複数決済・銀行引落しの候補統合と確認UI | 購入証拠の候補提示と複数原本を同じ購入へ手動紐付けるUIを実装済み。貸付・返済・口座振替・カード引落し・外部入金の銀行明細行も種別・金額・日時が一致する既存記録を候補から選び、サーバーで内容を検証して紐付ける。異文書の返金行はsource-backedなCorrectionだけをRelay cursorで候補表示し、確認を追加する。`TestCorrectPurchaseFromStatementKeepsRowEvidenceAtomic`で複数ページ、重複確認、不一致拒否、訂正・原本証拠を増やさないことをTestcontainers PostgreSQL 18で確認する。 |
| Amazonとメール等の取り込み | スクリーンショット・PDF・CSV・メール転送の取り込み処理と公式API連携の可否 | メール添付のPDF・JPEG・PNG・WebPから家庭単位の子文書と抽出jobを作り、親子関係を保存するAPIを実装。添付のみの合成EMLをTestcontainers PostgreSQL 18でPOSTし、親・子文書、両job、親子relation、API応答を検証。メール添付のsourceKindを同一transactionで変更・再解析するAPIと種別選択UIを実装し、queued job再利用と新しいjob versionを統合テストで確認。Stalwartの構成とIMAPS受信は保留中。個人向け購入履歴APIは公式資料で確認できず、SP-APIのOrders APIは販売者向け。Amazon Business Reporting APIは日本で注文レポートを提供するが、Businessアカウント、API利用承認、Analytics roleが必要なため、個人向け連携には使えない。一般ユーザー向けにはメール・PDF・画像・スクリーンショット・データexportを入力手段として維持する。参照: https://developer-docs.amazon.com/sp-api/docs/onboarding-overview、https://developer-docs.amazon.com/sp-api/docs/orders-api、https://docs.business.amazon.com/docs/reporting-api-overview、https://docs.business.amazon.com/docs/retrieving-order-reports、https://docs.business.amazon.com/docs/amazon-business-roles |
| 訂正学習と商品正規化 | 商品概念・バリアント・店舗別名、訂正の適用範囲、検索候補の改善 | 商品概念・バリアント・店舗別名の登録、明示候補選択、店舗別名の訂正学習と商品負担ルールをローカル画面で確認 |
| AIジョブと評価 | 失敗後原本保持、リトライ、モデル版・プロンプト版・費用記録、評価データと指標 | Free/PaidルーティングとSpend Cap時の保留を合成API・DBで検証し、合成画像による実Gemini呼び出しも両ルートで確認済み。2026年10月14日にPaid Projectへの隔離した単発呼び出しを確認した後、検証用リソースを削除した。500円Spend Capの設定後、Infra sync wave調整で通常WorkerのGemini処理が実環境に反映され、`GEMINI_ENABLED=true`、Worker Ready、関連ExternalSecretの同期成功を確認。無料Projectの429は有料へ即時切替せず、`Retry-After`に従ってWorkerが無料APIを再試行する。共有RPM・TPM・RPD予約を実装し、route/model別上限を`GEMINI_QUOTA_LIMITS`で設定できるようにした。値を未設定のroute/modelでは予約制御されない。複数Workerの同時予約、token実績反映、429 cooldownとの併用をPostgreSQL統合テストで確認した。RPD予約は`America/Los_Angeles`の暦日境界でリセットし、夏時間を含む境界をテストで確認した。失敗応答でもUsageMetadataから取得できるtoken数と推定費用を記録する。オフライン評価器は保存済みReceiptごとのモデル別精度・推定費用・記録済み遅延を出力する。 |
| 帳簿の検索 | 購入・品目切替え、店舗・商品・カテゴリ・期間・金額・負担・決済・証拠状態の検索 | `PurchaseFilter`へ条件をまとめ、金額・日付・文字列・分類・負担・決済・証拠状態をページング前に検索する。ITEM対象では金額・分類・タグ・プール・検索語を同一品目へ適用し、該当品目だけを返す。LedgerPageの二重filterと検索用CSSをpage sliceへ移した。`" Wanted "`のような前後空白を含む検索と品目条件の組合せをDB統合テストに追加した。公開環境では2026年10月13日に購入検索を確認済み |
| 分析と予算 | 月次・年次、カテゴリ・個人負担・店舗・商品価格、定期支出・予算対実績、口座変動の分離 | 月次・年次、カテゴリ・店舗・品目・プール・決済元の集計、購入時点のメンバー負担スナップショット、定期支出の月額換算、月次予算対実績、口座残高の期間別仕訳分析をWebソースに実装済み。カテゴリ別月次推移は上位5カテゴリとその他を表示し、正額を基準線より上、返金・訂正による負額を下に表示する。月ごとのカテゴリ内訳と差引合計を`role="img"`と`aria-label`で提供する。Frontend型検査、lint、フォーマット検査は通過。バックエンドのスナップショットAPI focused DBテストは通過。古い購入に負担スナップショットがない場合は現行比率を推定せず、APIの欠落フラグを表示する。商品分析は実効単価・印字単価の推移と単価不明件数を表示する。2026年10月13日07:39 UTCに公開環境の同じ家庭で6か月間を選び、3件の下書き購入がカテゴリ・店舗・品目・負担先・決済元・メンバー負担の全集計軸で期間合計0円、月平均0円、記録なしとして扱われることを確認。対象イメージは`v0.0.0-4e119145357fe4b6b7c662f4d7791d0e4eba93d6-117`。
| 認証と秘密管理 | ZITADEL Code+PKCE、state・nonce、HttpOnly/Secure Cookie、CSRF、基盤の保存方針に従った非公開原本保存 | 公開環境のOIDCログイン成功を確認。アプリのSSE指定は撤去し、保存基盤の管理に従う |
| フロントエンドの構造 | FSDのレイヤー・スライス公開API、Steiger検証、piccaとのライブラリ選定対応 | app・pages・sharedの構成と公開APIを実装。単一ページだけで使う招待管理、残高推移、品目入力、分析・帳簿・取り込み・購入登録・精算画面の状態とUIは所有するpages slice内へ分割し、再利用されないfeature sliceは作らない。分析・購入登録専用のstylesheetを各page sliceへ移した。今回の画面分割を含むWeb型検査、Steiger、lint、format、buildが成功 |
| GraphQL API | PiccaのSDL分割、Relay Node/双方向Connection、Mutation Result、nullability、Global ID | SDLを独立ドメイン型とMutationごとに分割し、全MutationをSuccess/UserError unionで返す。共通`MutationError` interfaceとドメイン上のinterface/unionを使い、AccountBalanceChangeの発生元はGlobal ID付きNode、Settlementは家計と会計月で識別するNodeにした。未完了だったGraphQLレビューは解決済みとして削除した。`ProductVariant.brand`と`packageSize`は未設定値をnullに統一し、明白な不要nullableがないことを確認した。Budget、Tag、RecurringExpense、ProductCatalogはConnectionで継続読込できる。画面が設定項目や商品候補の全件を必要とするためFrontend read関数は全ページを取得し、この判断は`docs/decisions/review-responses.md`に記録した。Relay/gqlgen生成と`go test ./internal/graph`は成功 |
| コンテナとGitOps | 再現可能なCIビルド、GHCRイメージdigest、infra生成とArgoCD同期 | Horse commit `74037b556569516385c0999e98984cc525373a28`のコードからBuildxで作成した`v0.0.213`を公開。infra commit `e06697647a6b52afc74236fa6b3208023867e095`でAPI・Worker・Web・OCRをdigest固定し、Argo CD Application `horse`は`Synced`・`Healthy`。4 Deploymentはすべて1/1 Ready。`https://horse.walnuts.dev/`と`/healthz`はHTTP 200。 |
| セキュリティと運用 | NetworkPolicy、非公開原本、監視・ログ・トレース、基盤によるSeaweedFSとDBのバックアップ | NetworkPolicy・非公開原本の実装あり。Googleの月500円Spend CapがCONFIGUREDで、50%・80%・100%の通知設定を確認。Horse専用バックアップは設けず、基盤の全体バックアップに従う |
| 公開サービス | kurumiの各Pod Ready、正しいTLS、公開URL、OIDCログイン後の主要操作と永続化 | `https://horse.walnuts.dev/healthz`と`/`はHTTP 200。公開環境の合成家庭でTanStackのファイルベースrouteを使い、帳簿・精算・分析・取り込み・設定を直接開いてURLと画面見出しを確認し、各routeを再読み込みしても同じ見出しが表示されることを確認した。ログアウト状態で`/households/00000000-0000-0000-0000-000000000001/ledger?month=2026-10`を直接開き、pathnameとsearchが`returnTo`に保持されることも公開ブラウザーで確認した。このUUIDはroute保持確認用で、家庭の実データは取得していない。 |

2026年10月12日14:38 UTC頃の公開監視確認で、VictoriaMetricsの`probe_success{target="horse"}`は1を返した。Tempoの直近1時間を対象に`{resource.service.name="horse-api"}`を検索し、`horse-api`をroot serviceとするトレースを3件確認した。Workerトレースは0件だったが、その時間帯はGeminiが無効でCSV原本も未アップロードだったため、抽出ジョブがclaimされていなかった。Workerの処理成功・失敗はこの結果から判定していない。


## ローカル検証の記録

2026年10月12日に`mise run test`と`mise run lint`が成功した。
PostgreSQL18の専用ローカルDBを指定した`HORSE_TEST_DATABASE_URL=… mise run test`では、以下の統合テストも成功した。

- `TestAccountingFlowUsesScheduleAndActualFundingSeparately`で3対2の500円配賦、予定入金と実入金の分離、未入金4000円の精算への反映を確認した。
- 同じ会計統合テストで異なるフィールドの更新統合、同じフィールドの競合、配賦の更新失敗時に金額も元に戻ること、原本と明細参照による購入登録の冪等性を確認した。
- `TestHouseholdIsolationForGraphQLAndOriginalDocument`で、非所属メンバーのセッションから対象家庭と原本を取得できないことをAPI経由で確認した。
- 税計算の純粋関数テストで内税額・税率別小計の照合、税グループの区分矛盾、同じ合計になる複数の解釈候補を確認した。
- 配賦の5秒間のfuzz実行で1,788,898件を検証し、配賦後の合計保存を確認した。

PP-OCRv6のamd64コンテナをローカルで起動し、合成した英語レシート画像から8行の文字列、信頼度、正規化矩形を取得した。
これはOCRサービスの推論経路の確認であり、日本語レシートの精度評価ではない。
FrontendのRelay生成、型検査、Steiger、Oxlint、Stylelint、フォーマット検査とSSRビルドが成功した。

ローカルChromeでは、家庭・プール・決済元・カテゴリの作成と再表示、Enterによる品目追加、TSV複数行貼付けを確認した。
390×844の画面幅でも、ヨーグルト198円、牛乳248円、パン100円の3品目を保存し、帳簿で合計546円と各品目・負担先を確認した。
この操作は専用テストDBと一時セッションを使用しており、公開環境のOIDCログインを検証したものではない。
購入の保存後に店名編集が拒否される不具合を修正し、記録済み購入の店名変更が保存されることを確認した。
複数行貼付け後に「元に戻す」を押すと、追加した品目がまとめて取り消されることを確認した。

ローカルSeaweedFSへ2行のカードCSV原本をアップロードし、Geminiを未設定のワーカーが原本を取得して明細を抽出することを確認した。
取り込み画面で保存済み文書を再表示し、777円と333円の明細を別々に購入登録して帳簿で確認した。
画面を再訪して同じ明細を再登録しても、帳簿の購入が重複しないことを確認した。
この確認はローカルの原本保存、非同期処理、確認画面、帳簿への登録経路を対象とし、公開環境のS3やGeminiによる解析は対象としていない。

ローカルのアプリコンテナを読取専用filesystemで起動し、SSRと参照される3件のJS/CSSがHTTP200、APIのreadinessがHTTP200になることを確認した。
空のDBへのAtlas適用で発見したテーブル定義の重複を修正し、新規の`horse_validation2`へ全7マイグレーションを適用できることを確認した。

原本の確認APIでは、指定した解析ジョブの品目と座標を証拠リンクに保存する。
会計APIの所属検証テストで、存在しない解析項目を指定した確認は購入との対応も残さずロールバックされ、確認後に解析結果を変更しても印字内容と原本座標を参照できることを確認した。
コミット`9432ac1`のGitHub Actionsでは、空のPostgreSQL18へのマイグレーション、Goの統合テストとlint、OCRのlint、Frontendの生成物・型・lint・FSD・フォーマット検査とビルドが成功した。

IndexedDBへ下書きと送信待ち購入を保存する処理を追加した。
ローカルAPI停止中に123円の購入を保存し、送信失敗として保持された購入がタブの再読み込み後も残り、API復旧後の再送で帳簿へ1件だけ登録されることを確認した。

GeminiのFree/Paidルーティングとジョブ再試行は、外部Geminiを呼ばないhttptestと専用PostgreSQL18の`horse_worker_validation`で検証した。
全マイグレーションを適用したDBで`mise run test`が成功し、`mise run lint`は0件だった。
Spend Capでは通常エラーの失敗回数を増やさず、claim回数が9回を超えてもジョブを保留できること、通常503は5回で失敗扱いになること、Gemini無効時は画像解析を未claimのまま保持しCSVは成功することを確認した。
ローカルAPIとWorkerにもこのコードを反映し、Workerは`GEMINI_ENABLED=false`で起動している。

2026年10月13日に、合成レシートだけをGoogle Gemini APIへ送り、FreeとPaidの各ルートを1回ずつ呼び出した。両方で品目264円と242円、合計506円が返り、Paidのtoken metadataから求めた公式標準料金の概算は$0.00230175だった。この金額は請求確定値ではない。検証器は`Balanced=false`を返し、税区分の整合を確認できなかったため、成功した税計算とは扱わない。実行環境からKubernetes Secretをプロセス入力へ渡し、値の表示・保存・ログ出力は行っていない。

ローカルの合成家庭で商品概念、1Lのバリアント、店頭別表記を登録し、別表記から候補を検索して明示選択後に2円の購入を保存した。
帳簿編集で`MERCHANT_ALIAS`を選び、店舗別表記が商品設定へ反映されることと、バリアント単位の負担ルールを保存できることを確認した。
招待を2件発行して両方失効し、発行時だけトークンを表示すること、受信時にURL fragmentを除去して明示受諾ボタンを表示することを確認した。
コミット`ff9b343`でこれらの画面とRelay生成物を反映し、GitHub Actionsの再検証を開始した。

2026年10月12日に公開環境へOIDCログインし、「公開環境の動作検証」家庭の作成・再表示を確認した。
家庭内に「公開検証プール」と「公開検証銀行」を追加し、1,000円の「動作検証用入金」を記録した。
初回は残高分析が0円・仕訳なしのままだったため、口座入金・振替後に残高分析を再取得するFrontend修正を実装した。
6cb5359の配備後に100円の送信待ち購入を同じ冪等キーで再送し、成功後に帳簿へ1件だけ表示されること、ページ再読み込み後も1件のまま残ることを確認した。
公開検証用銀行へ外部入金1円を追加すると、口座残高が1,000円から1,001円へ即時更新され、口座分析にも入金+1円と期末残高1,001円が表示された。
銀行50円・現金51円の計101円購入を登録し、再読み込み後に2つの決済内訳、101円の品目、共用プールへの配賦が維持されることを確認した。

口座分析のmutation後再取得と購入送信の修正を含む6cb5359では、`mise run web:check`、`mise run web:lint:fsd`、`mise run web:lint`、`mise run web:format:check`、`mise run web:build`が成功した。公開環境で送信待ち購入の同一冪等キー再送、再読み込み後の単一登録、口座残高分析の即時更新も確認した。
2026年10月12日にメンバー負担スナップショット、未照合候補の検索、購入・品目単位切替えを追加したWeb差分について、Relay再生成では51件のreader、normalization、operation textを生成し、`mise run web:relay:validate`、`mise run web:check`、`mise run web:lint:fsd`、`mise run web:lint`、`mise run web:format:check`、`mise run web:build`が成功した。変更は`7b10ede`までにpush済みで、品目の実効単価・印字単価の推移は現行コードで実装済み。

資金拠出設定と精算では、新規DB`horse_accounting_validation`へ全12マイグレーションを適用し、Goの全テストとlintが成功した。
`TestSettlementUsesPoolFundingSnapshotAndCarriesClosedBalanceOnce`で、拠出比率60対40の共用口座による1000円のA個人支出がAの400円とBの−400円の月次調整になること、締め後のpolicy変更がスナップショットを変更しないこと、翌月残高へ一度だけ繰り越すことを確認した。
純粋関数では1000円の全額補填方式と個人口座による他メンバー分の立替控除も確認した。
`TestAccountingPeriodsUseJapanTimeBoundaries`で、日本時間の9月30日23時59分59秒と10月1日0時を別の会計月として扱うことを確認した。
ローカル画面では個人プールの所有者設定と再表示、精算の全内訳と0円残高の表示を確認した。

## ライブラリの基準

piccaの現行実装をライブラリ選定の参考とする。
対応する責務にはAtlas、Bob、pgx、gqlgen、OIDC、AWS SDK v2、RabbitMQクライアント、TanStack Start、React、Relay、Base UI、IndexedDB、OpenTelemetryを検討し、独自実装が必要な会計・税解釈・照合ロジックとは区別する。
フロントエンドはFeature-Sliced Design v2.1に従う。

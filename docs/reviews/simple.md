# Horse コード品質レビュー

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse) の `main` ブランチ（2026年10月14日確認）

## 総評

BEはかなり大きく整理し直す価値があります。 問題は単に長い関数が多いことではなく、責務を分ける境界が曖昧なため、一つの機能を変更する際に理解しなければならないコードが多すぎることです。

特に深刻なのは次の4点です。

1. Application層の処理が巨大化している。 入力検証、権限確認、ロック、DB読み書き、業務計算、監査イベント、仕訳同期が同じ関数に混在。
2. GraphQL層がデータ取得と表示モデルの組み立てを抱えすぎている。 必要なフィールドだけ取得するGraphQLの利点を十分に活かせていない。
3. HTTP/API層に業務処理が入り込んでいる。 アップロードやメール取り込みの処理がHTTPハンドラと密結合。
4. 似た処理が機能ごとに再実装されている。 通常更新・一括更新、文書アップロード・メール転送取り込みなどで、処理の共通部分と差分が整理されていない。

一方で、BobによるDBコード生成、gqlgen、Kessoku、会計・配賦計算の一部の分離は活かすべきです。全面的に別のフレームワークへ移行する必要はありません。 むしろ既存ライブラリをもっと徹底して使い、自前の接着コードを減らす方が効果的です。

## 最優先で直したい箇所

### 1. 購入更新の責務を根本的に整理する

優先度：最高 / 改善効果：非常に大きい

該当コード：

- [purchase/update_item.go — updateItem()](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/update_item.go)
- [purchase/service.go — create()](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/service.go#L189-L453)
- [purchase/correction.go — Correct()](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/correction.go#L250-L606)
- [purchase/update_operations.go](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/update_operations.go)

`f106383`では`updateItem()`の入力検証、更新対象の取得とロック、計画構築をそれぞれヘルパーへ分離しました。商品学習の設定解釈や履歴記録も`item_product_learning.go`にあります。

一方、`buildPurchaseItemUpdatePlan()`は商品バリアントや支払情報などをDBから取得しながら計画を構築します。`updateItem()`は計画後の商品学習、明細・配賦・支払の更新、購入revisionの更新、監査イベント記録、仕訳同期をトランザクション内で順に調整します。読み込みを伴う計画構築と適用順序の責務をどこまで分けるかは、競合検出とロック順序を保った上で検討できます。

### 2. GraphQLの巨大な一括組み立て処理を見直す

優先度：最高 / 改善効果：非常に大きい

該当コード：

- [graph/helpers.go — loadHousehold()](https://github.com/walnuts1018/horse/blob/main/internal/graph/helpers.go#L104-L264)
- [graph/helpers.go — loadPurchases()](https://github.com/walnuts1018/horse/blob/main/internal/graph/helpers.go#L284-L466)
- [graph/queries.go](https://github.com/walnuts1018/horse/blob/main/internal/graph/queries.go#L15-L75)
- [graphql/household.graphql](https://github.com/walnuts1018/horse/blob/main/graphql/household.graphql)

`loadHousehold()`は基本情報と詳細データの取得を分け、GraphQLで要求されたmembers、pools、accounts、categories、defaultAllocationだけを読み込みます。選択されていないHousehold詳細まで一括取得する問題は改善済みです。

一方、`households()`と`viewer()`は所属情報を取得した後、`loadHouseholds()`でHouseholdの基本情報と要求された詳細をまとめて取得し、所属情報の順序に合わせて結果を組み立てます。resolverの選択fieldに応じた専用loaderを各フィールドに用意しておらず、Household以外の親子関係を含む要求を汎用的にbatchする仕組みもありません。特にNode解決や複数親から同じ関連データを参照するクエリについて、クエリ数と取得範囲を確認し、必要ならBobの一括取得を使うbatch loaderを追加する余地があります。

Member、Account、Category、Household、Pool Nodeの複数ID解決では、所属確認と対象データ取得をBobで一括取得するよう変更しました。重複IDを含む順序維持、家庭所属の認可、CategoryのParentID mappingをfocused integration testで確認しています。単一LedgerEntry Nodeもbatch loaderへ通し、既存テストでLoanBalanceを含む型を解決できることを確認しました。`nodes(ids:)`で複数のUnmatchedObservation Nodeを取得する経路も、所属家庭、SourceObservation、DocumentConfirmation、必要なCorrectionをまとめて取得するよう変更しました。複合Node IDの重複、順序、未存在行、家庭所属の認可をfocused integration testで確認しました。Settlement Nodeは個別解決のままです。

また、`loadHousehold()`と`loadPurchases()`は取得データからGraphQL表示モデルを組み立てる処理も担っています。Householdの未要求詳細取得は解消しましたが、表示モデル組み立ての責務や`loadPurchases()`の大きさ、残るNode解決のbatchingは引き続き見直し対象です。

目指すのは「すべてを先読みする巨大なローダー」でも「フィールドごとに1件ずつSQLを実行する構造」でもなく、GraphQLで要求されたデータだけを、親子関係に応じた適切な単位で一括取得する構造です。

## 4. 通常更新・一括更新・まとめて保存の責務境界

優先度：最高 / 改善効果：非常に大きい

関連ファイル：

- [purchase/update.go](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/update.go)
- [purchase/bulk_update.go](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/bulk_update.go)
- [purchase/bulk_update_items.go](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/bulk_update_items.go)
- [purchase/save_item_edits.go](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/save_item_edits.go)

明細更新では`updateItem()`と`updateBulkItem()`が`itemChangePlan`を共有し、リビジョンと配賦計算の一部を共通化しました。購入全体の通常更新と一括更新も`purchaseUpdatePlan`と`applyPurchaseUpdatePlan()`を共有し、フィールドリビジョンの競合判定と購入行・支払行の更新を共通化しました。入力フィールドと支払額の検証には共通ヘルパーを使いますが、対象の読み込み、ロック、一括更新の事前検証と適用順、仕訳同期、監査イベント記録は各ユースケースが調整します。

さらに`SaveItemEdits()`では、各明細について`updateItem()`を呼び、その後に購入全体の`update()`を呼んでいます。

`SaveItemEdits()`は`planSaveItemEdits()`でIDとコマンドを正規化し、明細のlock順序を決めた後、`applySaveItemEdits()`で既存のtransactionフローを実行するよう分けました。個別明細と購入の業務変更計画は引き続き`updateItem()`と`update()`がそれぞれ構築するため、計画構築と適用の完全な共通化は未完了です。既存の複数明細を一括保存するDB統合テストは通過しています。

残る設計課題は、共有済みの計画・適用処理と各ユースケースに残る事前検証、ロック、イベント、仕訳同期の責務境界です。通常更新と一括更新では対象件数や適用順、仕訳同期の条件が異なるため、共通化する業務ルールとユースケース固有の調整を明確にした上で、重複や意図しない差異を見直す必要があります。単一更新と一括更新の流れは、例えば次の構造で整理できます。

Create / Update / BulkUpdate / SaveItemEdits

GraphQLなどの公開API

共通の変更計画の構築

差分判定・競合判定・配賦と支払の整合性検証

トランザクション内で変更計画を適用

ロック → DB更新 → 必要なら仕訳再構築 → イベント記録

ただし、既存のロック順序と会計期間の整合性は慎重に維持する必要があります。

## 5. その他の重要な改善点

| 対象 | 確認した問題 | 推奨する改善 |
| ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- | --------------------------------------------------- |
| [振替・貸付処理](https://github.com/walnuts1018/horse/blob/main/internal/application/transfer/record.go#L70-L489) | 入力ルール、仕訳明細、精算調整の計算は純粋な関数へ分離され、`Do()`にはDB操作、権限確認、ロック、冪等性処理などが残る | ユースケース処理の責務と順序を整理する |
| [Geminiクライアント](https://github.com/walnuts1018/horse/blob/main/internal/ingestion/gemini.go#L74-L313) | JSON応答の復号とusage抽出は純粋なdecoderへ分離されたが、HTTPクライアント設定、Prompt、API呼び出し、Quota処理は`ExtractRun()`に残る | Provider、Prompt、Quota処理の責務を整理する |

### 小さな自前ユーティリティも整理する

同様に、ID生成、UUID変換、世帯メンバー確認、入力値の検証が複数パッケージに存在しています。こうした小さな重複も、適切な共通責務に集約したいところです。

## 外部ライブラリは何を導入すべきか

ライブラリを増やすより先に、すでに導入済みのライブラリを活かしきれていない点を改善するのが効果的です。

| ライブラリ・技術 | 判断 | 理由 |
| ----------------------------- | --------- | --------------------------------------------------------------------- |
| 既存の `gqlgen` | より積極的に活用 | Field Resolverで必要なデータだけロードできる |
| 既存の `Bob` | 維持・活用 | 型付きDBアクセスとリレーション一括ロードがすでに利用可能。ORMを変える必要はない |
| dataloadgen | 条件付きで導入 | gqlgenのフィールド単位ロードでN+1が発生する箇所に有効。既存のBobの一括ロードで足りる部分では不要 |
| 既存の `pgx` トランザクションヘルパー | 利用を検討 | `Begin`・`Rollback`・`Commit`の定型処理を減らせる。ただし既存のキャンセル時Rollbackの扱いを維持すること |
| River・Watermill | 導入しない | 現行のDB正本とRabbitMQ通知を維持する判断と再検討条件を[`extraction-job-queue.md`](../decisions/extraction-job-queue.md)に記録 |
| 新しいORM・DIフレームワーク | 不要 | 既存のBob・Kessokuで十分。置き換え自体の複雑さの方が大きい |

ジョブ管理ライブラリの採否は[`extraction-job-queue.md`](../decisions/extraction-job-queue.md)に記録した判断に従います。

すぐに取り組む小規模リファクタリングというより、自前キュー実装を大幅に減らすための独立した設計課題として扱うべきです。

また、金額配賦や会計の計算に外部ライブラリを無理に導入する必要はありません。`internal/domain/allocation.go`の最大剰余法や`accounting/journal.go`の仕訳整合性検証は、責務が明確な比較的よい実装です。これらは維持する方がよいと考えます。

## 目指すべきBEの構造

全面的なディレクトリ移動ではなく、各層の責務を次のように明確化することを推奨します。

GraphQL / HTTP

認証済み入力の受け取り、型変換、レスポンス返却。業務判断はしない。

Application / Use case

一連の操作の順序、トランザクション、権限、ロック、ユースケースの調整。

Domain

配賦計算、金額や数量の不変条件、変更計画、仕訳の構築など、DBなしで検証できるルール。

Infrastructure / Persistence

Bob、PostgreSQL、S3、Gemini、RabbitMQまたはRiverなどの具体的なI/O。

すべてのDB操作にRepository Interfaceを作る、すべてのエンティティに専用Serviceを作る、といった形式的なClean Architecture化は勧めません。

Horseで重要なのは、ビジネスルールを読もうとしたときにSQLやHTTP処理に邪魔されないこと、そして一つのルールに一つの実装しか存在しないことです。

特に純粋な計算部分については、DBモデルやGraphQLモデルに依存しないようにするとテストもしやすくなります。

## 改善する順序

| 順位 | 作業 | 期待できる効果 |
| -- | ------------------------------------------ | -------------------- |
| P0 | Purchaseの購入全体と明細の通常・一括・複数明細更新を共通の変更処理へ統合 | 残る重複した業務ロジックを削減 |
| P0 | `updateItem()`・`create()`・`Correct()`の責務分離 | 最も複雑なコードの理解と保守が容易になる |
| P1 | GraphQLの巨大ローダー、残るNode解決の最適化 | 可読性だけでなくDBアクセスも改善 |
| P1 | Gemini Routerの実行計画とProvider処理を分離 | フォールバックロジックが追いやすくなる |
| P2 | Transfer・Settlementの残る業務調整を整理する | 複雑な会計処理の理解と拡張が容易になる |

### リファクタリング時に絶対に守りたいこと

購入・会計周辺は、単にコードが短くなれば成功というわけではありません。

特に、購入の同時更新、部分的なフィールド競合の解決、閉鎖済み会計期間、購入と訂正の関係、返金上限、二重実行防止、複数明細の一括保存、支払額と配賦額の一致は、変更前後で挙動が等しいことを検証する必要があります。

既存のIntegration Testは相当数あるので、これを活かしてリファクタリング前の振る舞いを固定するのがよいです。さらに、更新対象外のフィールドで不要な仕訳再構築が発生しないことや、GraphQLで未要求の関連情報が取得されないこともテストに追加すると効果を測りやすくなります。

本書は静的レビューを起点に、修正内容とfocused testの結果を追記しています。残る性能評価、並行更新時の競合制御、未対応Node種別の取得経路は引き続き検証が必要です。

## 結論

HorseのBEでまず改善すべきなのは、細かい命名やコードスタイルではなく、購入更新処理を中心とした業務ロジックの重複と、責務の混在です。

優れたコードを目指すなら、巨大な関数を小さな関数へ機械的に分解するのではなく、次の状態を目標にするべきです。

「この変更で何が起こるか」がコードから自然に読めること。

「配賦や仕訳のルールを変更したい」と思ったら、変更すべき場所が一意に分かること。

単一更新でも一括更新でも、同じ業務ルールが使われること。

GraphQLやHTTPの都合が、業務ロジックへ入り込んでいないこと。

特に`purchase`周辺は、現状の設計を部分的に整理するだけで済ませず、共通の変更計画と業務計算を中心に再設計することを強く推奨します。未リリースの段階で大きく改善するなら、今が適したタイミングです。

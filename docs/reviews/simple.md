# Horse コード品質レビュー

対象：[walnuts1018/horse](https://github.com/walnuts1018/horse) の `main` ブランチ（2026年10月15日確認）

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
- [purchase/service.go — create()](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/service.go#L174)
- [purchase/update_operations.go](https://github.com/walnuts1018/horse/blob/main/internal/application/purchase/update_operations.go)

`f106383`では`updateItem()`の入力検証、更新対象の取得とロック、計画構築をそれぞれヘルパーへ分離しました。今回、トランザクション内の明細・配賦・支払・購入revisionの更新、履歴と監査イベントの記録、仕訳同期を`applyPurchaseItemUpdatePlan()`へまとめました。適用順序は従来どおりで、既存Integration Testで明細revision、監査イベント、購入revisionを確認します。商品学習の設定解釈や履歴記録も`item_product_learning.go`にあります。

`Correct()`は入力検証、購入の認可・ロック、冪等性再生、訂正計画の構築、永続化に分かれました。明細更新計画では入力値と計算値の準備をDBアクセスから分離しましたが、配賦計画や支払情報の構築にはDBアクセスが残ります。

`create()`では入力値の正規化、認可、冪等性確認、会計期間ロック、証憑確認、既定配賦の読込、永続化を専用関数へ分けました。`create()`はこれらを呼び出して既存transaction内の処理順を調整します。通常更新、一括更新、複数明細保存も引き続き責務の整理が必要です。変更時は各ユースケースのtransaction境界、ロック順序、仕訳同期と監査イベントの実行条件を維持する必要があります。

### 2. GraphQL loaderの未解決課題

優先度：最高 / 改善効果：非常に大きい

該当コード：

- [graph/helpers.go — loadHousehold()](https://github.com/walnuts1018/horse/blob/main/internal/graph/helpers.go#L104-L264)
- [graph/helpers.go — loadPurchases()](https://github.com/walnuts1018/horse/blob/main/internal/graph/helpers.go#L284-L466)
- [graph/queries.go](https://github.com/walnuts1018/horse/blob/main/internal/graph/queries.go#L15-L75)
- [graphql/household.graphql](https://github.com/walnuts1018/horse/blob/main/graphql/household.graphql)

未解決のGraphQL課題は、`loadPurchases()`で`items`、`totalYen`、訂正金額などが要求されない場合に明細と訂正aggregateを省けるか判断することです。購入一覧では`paymentAllocations`、証憑情報、明細の配賦とタグを要求fieldに応じて読み込みますが、親field間で必要データを共有する他のqueryの取得範囲は網羅できていません。

`loadHouseholds()`と`nodes(ids:)`は複数親をまとめて取得しますが、異なるNode型や複数Household詳細を同時に要求した場合のPostgreSQL query数を測定していません。Settlementの未確定previewはNodeごとにServiceを呼び出すため、親数に比例するDBアクセスがないかtestcontainersで確認し、必要ならBob batch queryへ変更します。

## 4. 購入更新のtransaction責務

Create、CreateAndPost、Update、BulkUpdate、BulkUpdateItems、SaveItemEditsではtransactionの所有者とcommit単位が各ユースケースに分かれています。transaction開始・rollback・commitの共通責務をどこまで集約し、ユースケース固有の原子性を保つかが未解決です。

## 5. 小さな自前ユーティリティも整理する

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
| P0 | PurchaseのCreate、Update、BulkUpdate、SaveItemEdits間のtransaction境界と適用順の整理 | 競合制御と会計同期を保ちながら重複を減らす |
| P1 | GraphQLの巨大ローダー、残るNode解決の最適化 | 可読性だけでなくDBアクセスも改善 |

### リファクタリング時に絶対に守りたいこと

購入・会計周辺は、単にコードが短くなれば成功というわけではありません。

特に、購入の同時更新、部分的なフィールド競合の解決、閉鎖済み会計期間、購入と訂正の関係、返金上限、二重実行防止、複数明細の一括保存、支払額と配賦額の一致は、変更前後で挙動が等しいことを検証する必要があります。

既存のIntegration Testは相当数あるので、これを活かしてリファクタリング前の振る舞いを固定するのがよいです。さらに、更新対象外のフィールドで不要な仕訳再構築が発生しないことや、GraphQLで未要求の関連情報が取得されないこともテストに追加すると効果を測りやすくなります。

本書は静的レビューを起点に、修正内容とfocused testの結果を追記しています。残る性能評価、並行更新時の競合制御、Node解決のクエリ数、選択fieldに応じた取得範囲は引き続き検証が必要です。

## 結論

HorseのBEでまず改善すべきなのは、細かい命名やコードスタイルではなく、購入更新処理を中心とした業務ロジックの重複と、責務の混在です。

優れたコードを目指すなら、巨大な関数を小さな関数へ機械的に分解するのではなく、次の状態を目標にするべきです。

「この変更で何が起こるか」がコードから自然に読めること。

「配賦や仕訳のルールを変更したい」と思ったら、変更すべき場所が一意に分かること。

単一更新でも一括更新でも、同じ業務ルールが使われること。

GraphQLやHTTPの都合が、業務ロジックへ入り込んでいないこと。

特に`purchase`周辺は、現状の設計を部分的に整理するだけで済ませず、共通の変更計画と業務計算を中心に再設計することを強く推奨します。未リリースの段階で大きく改善するなら、今が適したタイミングです。

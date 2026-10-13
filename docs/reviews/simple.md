# Horse の保守性・シンプルさのレビュー

対象は [walnuts1018/horse](https://github.com/walnuts1018/horse) の `main`、コミット [`c6cb75d`](https://github.com/walnuts1018/horse/commit/c6cb75dfa9d376c6b1bd213decf0814faddf0144) です。

結論として、バックエンドの責務分離は改善されていますが、フロントエンドには明確な簡素化の余地があります。 特に「巨大な画面コンポーネント」と「Relay の型・データ管理を独自実装で包み直していること」が、今後の変更コストを増やしています。

## 1. 最優先：画面コンポーネントが巨大すぎる

実測した主なファイルは以下のとおりです。行数には JSX も含みますが、状態変数の数から見ても単なる表示量の問題ではありません。

| ファイル | 行数 | `useState` の数 |
| --------------------------------------------------------------------------------------------------------------------------------------- | ----- | ------------- |
| [settings-page.tsx](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/pages/settings/ui/settings-page.tsx) | 2,456 | 90 |
| [ledger-page.tsx](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/pages/ledger/ui/ledger-page.tsx) | 2,374 | 51 |
| [imports-page.tsx](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/pages/imports/ui/imports-page.tsx) | 1,505 | 27 |
| [purchase-create-page.tsx](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/pages/purchase-create/ui/purchase-create-page.tsx) | 1,165 | 20 |

例えば `SettingsPage` には、口座・プール・配賦ルール・商品カタログ・招待・タグ・予算・定期支出・資金移動・残高推移がすべて同居しています。

`LedgerPage` も、購入一覧、明細一覧、検索、複数選択、一括変更、取り消し、訂正、証拠書類、観測データの照合まで抱えています。

これは、機能追加時に関連のない状態や副作用まで把握しなければならず、保守しづらい構造です。

改善方針は、単純な行数分割ではなく、独立して変更できる機能単位への分割です。

例えば設定画面なら `AccountSettings`、`PoolSettings`、`AllocationRuleSettings`、`ProductCatalogSettings`、`PlanningSettings` のように分離します。各コンポーネントが自分のデータ取得・フォーム状態・保存処理を持ち、親の `SettingsPage` はセクションの配置だけを担当する形がよいです。

帳簿画面では、購入・明細の一覧表示と選択ロジックを再利用しつつ、編集フォーム、一括操作、訂正ダイアログ、証拠表示をそれぞれ独立させます。

逆に、すべての `useState` を巨大なカスタム Hook に移動するだけでは改善になりません。状態の所有者そのものを分離することが重要です。

## 2. 高優先：Relay を使っているのに、独自の型・変換層が厚い

該当箇所は [web/src/shared/api/household.ts](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/shared/api/household.ts#L1-L360) と、その後半の [GraphQL 通信処理](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/shared/api/household.ts#L406-L795) です。

現在は、Relay の生成型とは別に、`Purchase`、`PurchaseItem`、`Household`、`Pool`、`LedgerEntry` など、多数の手書き TypeScript 型を定義しています。

さらに、API の返却値を `normalizeGraphQLValue()` で再帰的に走査し、GraphQL の構造を独自の平坦なオブジェクトに変換しています。入力についても `normalizePurchaseVariables()` が操作名を文字列比較して、GraphQL の入力構造を組み替えています。

この設計には、次の問題があります。

- GraphQL スキーマと手書き型の整合性を人間が維持しなければならない。
- `as T` や `as Purchase` による型アサーションが、スキーマ変更の不整合を隠しやすい。
- データ取得、GraphQL の Union 判定、単位・数量の変換といった別責務が通信層に混在している。
- 特定の Mutation 名や `__typename` に依存する処理を、汎用的な関数が暗黙に実行している。

特に、[use-household-purchases.ts](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/entities/purchase/api/use-household-purchases.ts#L46-L76) でも同じ数量フィールドの平坦化を行い、最後に `as unknown as PurchaseItem[]` で型を合わせています。つまり、変換の責務が一元化されていません。

推奨する構造は次のとおりです。

- GraphQL 通信境界では Relay の生成型をそのまま使う。
- UI に本当に必要な形式への変換だけ、該当 Entity 内の明示的な mapper で実施する。
- GraphQL の数量入力を手書きのフィールド群から復元するのではなく、最初からスキーマに沿った型で組み立てる。
- `executeGraphql()` と `useGraphqlMutation()` にある Mutation 実行・エラー処理の重複を共通化する。

これにより、単にコード量が減るだけでなく、GraphQL スキーマを変更したときに TypeScript のコンパイルエラーで影響箇所を発見しやすくなります。

Relay をやめる必要はありません。むしろ、すでに採用している Relay の仕組みを素直に使う方が、実装を簡潔にできます。

## 3. 高優先：Go の Application 層と GraphQL 層の責務分担が一貫していない

Go 側は前回より改善されていますが、まだ過渡的な構造が残っています。

典型例が、[purchase/update.go](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/application/purchase/update.go#L31-L205) と [graph/update_purchase.go](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/graph/update_purchase.go#L11-L29) です。

購入更新の Application Service は、次のコールバックを GraphQL Resolver から受け取っています。

```
type UpdateOperations struct {
    LockAccountingPeriods func(...)
    ReplacePayments       func(...)
    SyncAccounting        func(...)
}
```

これは、Application 層へ処理を移したものの、肝心な処理の実体を GraphQL 層に残している状態です。

問題は、購入更新処理を理解するために、Application 層と GraphQL 層を行き来しなければならないことです。また、Application Service 単独では必要な依存が揃いません。

一方、[update_purchase_item.go](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/graph/update_purchase_item.go#L29-L180) や [mutations_purchase_correction.go](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/graph/mutations_purchase_correction.go) には、DB トランザクション、競合制御、会計処理などが直接書かれています。

改善案：複雑な書き込み処理は Application Service 側で完結させる。

GraphQL Resolver に残すのは、認証情報の取得、GraphQL 型からコマンドへの変換、Service の呼び出し、結果の GraphQL 型への変換だけで十分です。

特に `UpdateOperations` のように GraphQL 層の関数を Application 層へ渡す構造は、期間ロック・支払更新・仕訳同期を共有可能な内部処理へ移し、取り除くことを推奨します。

ただし、単純なマスターデータの CRUD まで無理に Service 層へ移す必要はありません。複雑な取引処理に限定して責務分離を徹底するのが、最も簡潔です。

## 4. 高優先：購入登録が依然としてフロントエンドの複数操作になっている

[create-and-post.ts](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/entities/purchase/api/create-and-post.ts#L11-L40) では、購入登録を次のように実装しています。

```
createPurchase
    ↓
postPurchase
    ↓ 失敗したら
createPurchase で再取得して状態確認
```

取り込み画面では、これにさらに証拠書類の紐付けが続きます。

[imports-page.tsx の確認処理](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/pages/imports/ui/imports-page.tsx#L589-L677)

これは単に Mutation が二つあるという問題ではありません。

クライアントが中間状態・リトライ・冪等性・部分成功を管理する必要が生じ、その複雑さが通常入力と AI 取り込みの両方へ波及しています。

バックエンドに通常登録用の単一コマンドを用意し、購入作成から記帳まで一つのトランザクションで完結させる方が、フロントエンドもバックエンドも単純になります。AI 取り込みでは証拠書類との関連付けも同じトランザクションに含めます。

既存の `createPurchase` と `postPurchase` は、明示的な下書き操作に必要なので維持して構いません。

## 5. 中優先：Go の共通処理に重複実装がある

これは単なるコードスタイルの問題ではなく、会計処理の共通ルールが複数箇所に分散している点が気になります。

| 重複している処理 | 確認した実装 |
| -------------------------------- | --------------------- |
| `accountingPeriod` | GraphQL、購入、資金拠出、送金、精算 |
| `checkedAdd` | GraphQL、購入、送金、仕訳 |
| `writeEvent` | GraphQL、購入、資金拠出、送金 |
| `validateIdempotencyFingerprint` | GraphQL、購入 |
| 会計期間の作成・ロック | GraphQL、購入、送金 |

例えば、[purchase/service.go](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/application/purchase/service.go#L507-L536)、[transfer/record.go](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/application/transfer/record.go#L444-L488)、[graph/mutation_support.go](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/graph/mutation_support.go#L25-L67) には、ほぼ同じイベント記録や冪等性検査があります。

また、すでに [accounting/journal.go](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/application/accounting/journal.go) には貸借の検証と仕訳生成を行う `BuildEntries` / `InsertEntries` があるのに、[口座の初期残高](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/graph/mutations_account.go#L66-L117) と [口座間移動](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/graph/account_movements.go#L124-L145) では仕訳明細を手動で二行ずつ INSERT しています。

ここは既存の仕訳生成処理に寄せた方が、コードも短くなり、貸借不一致の防止ルールを統一できます。

ただし、何でも `utils` に集めるべきではありません。イベント記録、会計期間管理、冪等性検査、金額計算のように、ルールが一貫していなければならない処理だけを共通化するのが適切です。

## 6. 中優先：帳簿の検索・編集処理が UI に密結合している

[ledger-page.tsx](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/pages/ledger/ui/ledger-page.tsx#L656-L900) では、購入単位と品目単位で非常に似た検索条件を別々に実装しています。

さらに、検索条件が有効になると、[use-household-purchases.ts](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/entities/purchase/api/use-household-purchases.ts#L173-L178) の `loadAll` によって全ページを自動取得し、ブラウザで絞り込みます。

この方式は検索項目が増えるほど UI の分岐と取得処理が複雑になります。

できればフィルタ条件を一つの明示的な型として扱い、サーバーへ渡して検索とページネーションを実行する構造にしたいです。購入表示と品目表示で解釈が異なる条件だけ、個別に扱います。

これにより画面側の `filter` / `flatMap`、全件取得制御、選択対象の整合性確認などを減らせます。

## 7. 中優先：CSS とフロントエンドのテスト構成

[global.css](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/app/styles/global.css) は約 4,655 行で、画面固有のスタイルまで単一ファイルに集約されています。

共通デザイントークンと汎用 UI はグローバルに残し、帳簿・設定・取り込みなどの画面固有スタイルは各ページ側へ配置する方が、変更範囲を把握しやすくなります。

また、現時点の `web` ディレクトリには `.test.ts` / `.spec.ts` などのテストファイルが見当たらず、[package.json](https://github.com/walnuts1018/horse/blob/c6cb75d/web/package.json) にもテスト実行スクリプトがありません。

一方、Go 側には多数の単体・統合テストがあります。

フロントエンドにテストを大量追加する必要はありませんが、購入データの変換、IndexedDB の Outbox、複数選択の取り消し、レシート領域の座標変換など、状態遷移や計算を伴う処理は、UI から純粋関数に分離してテストしたいところです。

特に [imports-page.tsx](https://github.com/walnuts1018/horse/blob/c6cb75d/web/src/pages/imports/ui/imports-page.tsx#L110-L147) にある行列演算や領域座標変換は、画面コンポーネント内に置く必要がありません。

## 8. 低〜中優先：GraphQL の読み取り処理が一部過剰に結合している

[loadPurchases](https://github.com/walnuts1018/horse/blob/c6cb75d/internal/graph/helpers.go#L290-L508) は購入データの読み込み時、支払情報・訂正・証拠・商品明細などの関連情報を一括ロードし、GraphQL モデル全体を組み立てています。

Bob のバッチ読み込みを使っている点は良いですが、GraphQL の呼び出し側が一部のフィールドしか要求しなくても、読み取り処理が広範囲に実行されます。

これは現時点で全面改修が必要なほどの問題ではありません。ただし、購入一覧と購入詳細の用途が増えてくると、巨大な `loadPurchases` の変更がさまざまな API に影響します。

将来的には一覧用と詳細用の読み取りモデル、あるいは必要なフィールドだけを解決する構造を検討する価値があります。現段階では、安易に DataLoader を大量導入するより、既存のバッチ読み込みを維持する判断も十分合理的です。

## 改善の優先順位

| 優先 | 対象 | 推奨する対応 |
| -- | --------------------------- | --------------------------- |
| P1 | 巨大な React 画面 | 機能単位のコンポーネント・状態分割 |
| P1 | Relay と手書き型の二重管理 | 生成型中心へ整理し、暗黙の変換を削減 |
| P1 | Application / GraphQL の責務混在 | 複雑な書き込み処理を Application 側で完結 |
| P1 | クライアント側の複数 Mutation | 一回で記帳できる API に集約 |
| P2 | Go の会計関連共通処理 | 仕訳・期間管理・冪等性の重複を解消 |
| P2 | 帳簿の絞り込み・全件取得 | 条件を型として統一し、DB 側で絞り込み |
| P2 | 巨大 CSS と UI テスト不足 | スタイルを機能別に整理し、重要な純粋処理をテスト |
| P3 | 購入データの一括読み込み | 必要になった段階で読み取りモデルを分離 |

### 現状で維持したい設計

Go の純粋なドメイン計算、Bob による型付き DB アクセス、gqlgen/Relay のコード生成、会計の不変条件を守るトランザクション、リビジョンによる競合検知、Outbox の冪等性保証は、コードが長くなっても維持する価値があります。

また、生成コードが巨大であること自体や、GraphQL スキーマがファイル分割されていること自体は、保守性の欠点とは判断しません。

Horse で目指したいのは「コードの行数を減らすこと」ではなく、「一つの機能を変更するときに読むべきコードを減らすこと」です。

現状ではまずフロントエンドの画面構造と Relay 境界を整理し、その次に Go の書き込み処理と共通会計処理を整理する順序が、改修効果に対してリスクが小さいと思います。

なお、これは最新コミットの静的コードレビューです。リポジトリの変更は行っておらず、今回の調査でテストの実行や動作測定はしていません。

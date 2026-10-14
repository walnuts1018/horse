# Horse バックエンドの保守性・ライブラリ活用レビュー

対象は[walnuts1018/horse](https://github.com/walnuts1018/horse)の`main`です。2026年10月14日にレビュー指摘と現行の作業ツリーを照合しました。

購入作成・確定・更新・訂正、品目更新、入金・送金、口座間移動・外部入金、精算にはApplication Serviceがあります。月次支出・商品候補・口座残高分析は`internal/postgres/query`にあります。取り込みWorkerはclaim、抽出、履歴参照、結果保存、期限切れジョブ回収のファイルへ処理を分割しました。BobのリレーションロードとDSLを使用し、PostgreSQL統合テストもCIで実行します。以下は現行コードで確認した未解決事項です。

## 1. 口座残高分析の読込範囲

[`internal/postgres/query/account_balance_analysis.go`](../../internal/postgres/query/account_balance_analysis.go)に口座残高の読取と残高計算を移しました。履歴ページの各行には過去からの残高が必要なので、現在の実装はページサイズにかかわらず口座の全仕訳を読み込みます。Connection変換は[`internal/graph/account_balance_analysis.go`](../../internal/graph/account_balance_analysis.go)に残ります。

Query層への移行は完了しました。全仕訳の読込量削減は未実施で、性能計測もありません。集約による削減は全期間残高と安定した順序を保つ方式を定め、実測してから採用してください。

## 2. Bob Factory

`bobgen.yaml`ではFactory生成が無効です。統合テストはPostgreSQL上に明示的なFixtureを作るため、会計条件の明確さが保たれています。セットアップ重複を減らす具体的な必要がある場合に限り、家庭・メンバーなどの共通準備をFixture化してください。金額、負担条件、精算結果はテスト内で明示します。

## 現在のライブラリ判断

| 技術・ライブラリ | 判断 | 根拠 |
| --- | --- | --- |
| Bob | 維持 | CRUD、Query Builder、リレーションロードを使用 |
| Bob Query Code Generation | 不採用 | 生SQL文字列を使わず、Bob DSLで記述する |
| gqlgen | 維持 | SDLを機能別に分割し、ConnectionとNodeを使用 |
| pgx | 維持 | DB接続とトランザクションに使用 |
| Atlas | 維持 | マイグレーション管理に使用 |
| `google.golang.org/genai` | 維持 | Gemini公式SDKを使用 |
| OpenTelemetry | 維持 | pgx Poolに`otelpgx`を設定済み |

## 対応順

1. 口座残高分析の全仕訳読込を対象に性能を計測し、必要な場合だけ集約方法を改善する。
2. Fixtureの重複が明確なテストだけ共通化する。

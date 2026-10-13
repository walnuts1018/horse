# Horse

複数人で購入・負担・資金移動・精算を共有する家計プラットフォーム。
仕様は[plan.md](plan.md)、完成要件と検証状況は[完成要件と検証](docs/completion-audit.md)を参照する。

## 構成

- React、TanStack Start、Relay、Feature-Sliced Designのフロントエンド。
- Go、gqlgen、Bob、PostgreSQLのAPIと会計台帳。
- AtlasのDBマイグレーション。
- SeaweedFSの非公開原本、RabbitMQの取り込み通知、PostgreSQLの永続ジョブ。
- Geminiの直接画像解析と、独立したPP-OCRv6のCPU読取。
- ZITADELのAuthorization Code+PKCEと、HttpOnly CookieのDBセッション。
- infraリポジトリのArgoCDとEnvoy Gatewayによるkurumiへの配置。

ライブラリ選定はpiccaを基準とする。フロントエンドの対象ブラウザーは[ブラウザー対応方針](docs/browser-support.md)に定める。

## 開発環境

ツールは`mise install`で準備する。
フロントエンドの依存は`cd web && mise exec -- pnpm install --frozen-lockfile`で取得する。
環境変数の一覧は[.env.example](.env.example)を参照し、実行環境へ設定する。

PostgreSQLへ`DATABASE_URL`を設定して`mise run db:migrate`を実行する。
APIを`mise run api`、フロントエンドを`mise run web:dev`で起動する。
取り込みワーカーはS3とGeminiの設定を加えて`mise run worker`で起動する。
認証を伴う実操作には、開発用のコールバックURLを登録したZITADELクライアントが必要となる。

Geminiには無料・有料の別ProjectとAPIキーを設定する。
`GEMINI_ENABLED=true`を設定するまで、画像やメールの解析ジョブは待機し、カード・銀行CSVの取り込みだけを処理する。
Google側の月額Spend Capと警告メールはinfraのTerraformで管理する。
Spend Capによる停止中も、帳簿の閲覧と手入力は利用できる。

DBスキーマの変更後は`mise run hash:migrations`と`mise run db:generate`を実行する。
GraphQLスキーマの変更後は`mise run generate:graphql`と`mise run web:relay`を実行する。

## 検証

```sh
mise run test
mise run lint
mise run web:relay:validate
mise run web:check
mise run web:lint
mise run web:lint:fsd
mise run web:format:check
mise run web:format:graphql:check
mise run web:build
mise run ocr:lint
```

DB統合テストには、専用のテストDBのURLを`HORSE_TEST_DATABASE_URL`に設定する。
本番DBや既存サービスのDBを指定しない。

## 配置

mainのCIでアプリとOCRのGHCRイメージを生成する。
infraの`k8s/apps/horse/`でイメージのdigest、External Secrets、DB、原本ストレージ、HTTPRouteを管理する。
APIの起動前にAtlasでDBマイグレーションを適用する。
公開URLは`https://horse.walnuts.dev`。

完成判定には、各PodのReady、TLS、OIDCログイン、主要操作、家庭間認可、保存後の再読込、バックアップ復元を確認する。

# バックエンドライブラリの選定

ライブラリは`picca`の実装を参照し、現在のコードへの適用状況と判断理由を記録します。実装済みの項目はこの一覧から削除し、採用を保留する項目と未解決の課題を残します。

## 採用済み・実装中

| ライブラリ | 用途と状況 |
| --- | --- |
| `github.com/labstack/echo/v5` | `picca`と同じv5.3.1を導入し、APIとWorker health serverをEchoへ移行中です。既存の`http.Server`のtimeout、SSE、net/http handlerとの互換性を維持します。 |
| `github.com/mazrean/kessoku` | `picca`と同じv1.1.1を導入し、APIとWorkerの起動時依存をcompile-time injectorで組み立てます。 |
| `github.com/testcontainers/testcontainers-go/modules/postgres` | PostgreSQL 18のGraphQL統合テストで、外部DB URLがない場合のローカル起動に使います。CIで既存PostgreSQL serviceが指定される場合は再利用します。 |
| `github.com/gabriel-vasile/mimetype` | 既存依存を文書アップロードの内容ベースMIME判定へ使う変更中です。 |
| `github.com/go-playground/validator/v10` | Worker設定の構造検証に使用済みです。GraphQL入力へ一括適用する変更は、金額や会計期間などの業務検証と重複しない境界を確認してから判断します。 |

## 未解決の候補

| ライブラリ | 候補と現在の判断 |
| --- | --- |
| `github.com/emersion/go-message` | 注文メールのMIME、転送encoding、文字コード処理を標準ライブラリの自前再帰解析から移行する候補です。既存のサイズ・深さ・part数制限、添付通知、alternative選択を維持できるAPIを確認し、移行を進めます。 |
| `github.com/JohannesKaufmann/html-to-markdown/v2` | HTML注文メールの表・リスト構造をモデル入力に残す候補です。現在のHTML walkerはブロック間の改行を作るだけで、table cellの境界は保持しません。MIME移行と合わせて合成表メールの抽出結果を確認します。 |
| Bob Factory | 複数の統合テストにfixture構築の重複があります。Factory導入で必須値や会計条件が隠れないよう、DB fixtureの重複を見てから限定利用します。 |
| `testify` | 既存のテストは標準`testing`で期待値と失敗理由を直接示しています。複雑な値の差分を短くできる箇所で採用し、単純な比較の一括置換はしません。 |
| gqlgen DataLoader | 現在はBobの関連ロードで主要な一覧N+1を避けています。loader追加で重複取得が消える未対応経路が見つかった場合に追加します。 |
| Watermill | PostgreSQL jobがsource document登録、claim、retry、停止後の回復を担い、RabbitMQは起床通知として使われています。既存のDB job回復を残したままWatermillを加える効果が確認できないため、現時点では導入しません。通知配送方式の変更時に再評価します。 |

## 採用しない候補

| ライブラリ | 判断 |
| --- | --- |
| `github.com/go-chi/chi/v5` | HTTP serverを`echo/v5`へ統一するため導入しません。 |
| Bob Query Generation | DBアクセスをBob DSLに統一し、生SQL文字列を直接書かない方針を維持するため、SQL文字列を入力にするQuery Generationは使いません。 |
| 別ORM | Bobの型付きDSL、生成model、relation loaderを利用しているため、ORMの二重化はしません。 |

## 現在の実装境界

DB読み書きはBob DSLを使い、GraphQLはgqlgen、HTTPはEcho v5、PostgreSQL migrationはAtlasを使います。設定ファイルやCIの存在だけを検査するテスト、mock call順だけを確認するテストは追加しません。ライブラリの採用では、実際の業務経路で責務が減ることと、既存の入力上限・認可・会計不変条件を維持することを確認します。

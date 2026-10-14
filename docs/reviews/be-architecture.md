# バックエンド構成の未解決事項

対象は2026年10月14日時点の作業ツリーです。

## Application Serviceのtransaction管理

Application Serviceは`Begin`と`Commit`を明示し、deferから`context.WithoutCancel(ctx)`を使ってRollbackします。defer内のRollback失敗は`internal/application/txcleanup`で記録します。一部の統合テストは失敗時の更新取り消しを確認しますが、全Serviceの失敗経路は網羅していません。現時点で共通transaction抽象や`pgx.BeginFunc`への移行が必要だと示す不具合は確認されていないため、失敗時のテストが必要になった場合に再評価します。

# 抽出ジョブの永続化と通知

## 決定

PostgreSQLの`extraction_jobs`を抽出ジョブ状態の正本として維持し、RabbitMQはWorkerを早く起こすための通知に使います。文書とジョブを同じDBトランザクションで登録し、通知の失敗やRabbitMQの停止があっても、WorkerがDBを走査して処理を再開します。

この記録は、2026年10月13日時点のコード構成に基づく設計判断です。WatermillやRabbitMQをジョブ状態の正本にする移行は行いません。

## 理由

ジョブは文書・家庭・解析結果に結び付き、状態、試行回数、失敗回数、再試行可能時刻、結果をAPIから参照します。DBにジョブを保存すると、文書登録とジョブ作成を原子的に行い、`FOR UPDATE SKIP LOCKED`で複数Workerが重複claimせずに処理できます。Workerは期限切れの実行中ジョブもDBから回復します。

RabbitMQには永続quorum queueでジョブIDを通知します。通知を受け取れない場合もWorkerは10秒間隔でDBを走査するため、通知のpublishとDB commitを分散トランザクションで結ぶ必要はありません。通知障害時の遅延は許容し、処理状態の回復を優先します。

PiccaはHorseと同じ`amqp091-go`を直接利用しています。WatermillはConsumerやMiddlewareの共通化には使えますが、HorseのDB上のclaim、再試行時刻、停止後の回復を置き換えません。Watermill導入後もDBを正本にするなら、現在のジョブ管理と通知処理が併存し、責務や障害経路が減る根拠が不足しています。RabbitMQをジョブ正本へ変える案では、DB更新とpublishの二重書込みにTransactional Outboxが必要になり、長時間停止・日次Spend Cap・再試行履歴も含めた再設計が必要です。RiverはDBベースのジョブ管理を提供しますが、現在の実装を置き換える利点が確認できていません。

## 影響

RabbitMQ通知が失われた場合、次のDB pollまで処理開始が最大10秒程度遅れます。ジョブ状態と再試行はPostgreSQLに残り、RabbitMQ通知の重複や再配送はDBのclaim条件で処理を抑止します。PostgreSQL pollの負荷は現行のWorker数と処理量で許容します。

## 再検討

DB pollが実測でボトルネックになった場合、または複数種類の非同期処理で通知・middlewareの共通化が明確な保守効果を持つ場合に再評価します。再評価では、重複通知、publish失敗、RabbitMQ停止、Worker停止、長時間retry、Spend Cap、期限切れclaim回復を含む障害試験を行い、状態の正本と処理保証を先に定義します。

## レビュー対応

[`docs/reviews/be-library.md`](../reviews/be-library.md)には、PostgreSQLのジョブ管理を廃止し、RabbitMQとWatermillへ配送・再試行・DLQを移す案がありました。2026年10月13日に現行コードと障害経路を確認し、この移行案を採用しない判断をしました。現在のDB管理は文書登録との原子性、APIから参照するジョブ状態、長時間再試行、停止後の回復を担い、RabbitMQ通知は処理開始を早めます。Watermillを追加してもDBのclaimと回復処理は残り、RabbitMQを正本にするとDB更新との二重書込みやSpend Capを含む再設計が必要です。レビュー文書からこの移行提案を除き、判断理由と再検討条件をこのファイルに残します。

## 主な実装箇所

- [`internal/api/documents.go`](../../internal/api/documents.go)で文書とジョブを同じDBトランザクションに登録し、commit後にRabbitMQへ通知します。
- [`internal/ingestion/queue.go`](../../internal/ingestion/queue.go)で永続quorum queueへの通知を扱います。
- [`internal/ingestion/worker.go`](../../internal/ingestion/worker.go)でDB claim、再試行状態、期限切れジョブの回復を扱います。
- [`cmd/worker/main.go`](../../cmd/worker/main.go)でRabbitMQ通知と10秒間隔のDB走査を併用します。

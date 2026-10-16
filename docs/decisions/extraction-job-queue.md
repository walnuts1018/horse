# 抽出ジョブの永続化と通知

## 決定

PostgreSQLの`extraction_jobs`を抽出ジョブ状態の正本として維持し、RabbitMQはWorkerを早く起こすための通知に使います。文書とジョブを同じDBトランザクションで登録し、通知の失敗やRabbitMQの停止があっても、WorkerがDBを走査して処理を再開します。

この記録は、2026年10月13日に決定した設計をRust移行後のコードに照らして更新したものです。WatermillやRabbitMQをジョブ状態の正本にする移行は行いません。

## 理由

ジョブは文書・家庭・解析結果に結び付き、状態、試行回数、失敗回数、再試行可能時刻、結果をAPIから参照します。DBにジョブを保存すると、文書登録とジョブ作成を原子的に行い、`FOR UPDATE SKIP LOCKED`で複数Workerが重複claimせずに処理できます。Workerは期限切れの実行中ジョブもDBから回復します。

RabbitMQには永続quorum queueでジョブIDを通知します。APIはDB transactionをcommitした後にpublishし、通知のpublishとDB commitを分散トランザクションで結びません。WorkerはRabbitMQ通知からジョブIDを指定してclaimし、並行してPostgreSQLから通常のジョブclaimと期限切れleaseの回復を行います。DB claimは通常1秒間隔で再試行するため、通知障害時はDB scanが処理開始を補います。

PiccaはHorseと同じ`amqp091-go`を直接利用しています。WatermillはConsumerやMiddlewareの共通化には使えますが、HorseのDB上のclaim、再試行時刻、停止後の回復を置き換えません。Watermill導入後もDBを正本にするなら、現在のジョブ管理と通知処理が併存し、責務や障害経路が減る根拠が不足しています。RabbitMQをジョブ正本へ変える案では、DB更新とpublishの二重書込みにTransactional Outboxが必要になり、長時間停止・日次Spend Cap・再試行履歴も含めた再設計が必要です。RiverはDBベースのジョブ管理を提供しますが、現在の実装を置き換える利点が確認できていません。

## 影響

RabbitMQ通知が失われた場合、PostgreSQLの通常claimが次の1秒以内のpollで処理を補います。ジョブ状態と再試行はPostgreSQLに残り、RabbitMQ通知の重複や再配送はDBのclaim条件で処理を抑止します。WorkerはRabbitMQ通知と通常claimの双方からジョブを処理するため、claim条件とlease fencingが重複処理を防ぎます。

## 再検討

DB pollが実測でボトルネックになった場合、または複数種類の非同期処理で通知・middlewareの共通化が明確な保守効果を持つ場合に再評価します。再評価では、重複通知、publish失敗、RabbitMQ停止、Worker停止、長時間retry、Spend Cap、期限切れclaim回復を含む障害試験を行い、状態の正本と処理保証を先に定義します。

## レビュー対応

以前のレビューにはPostgreSQLのジョブ管理を廃止し、RabbitMQとWatermillへ配送・再試行・DLQを移す案がありました。2026年10月13日に現行コードと障害経路を確認し、この移行案を採用しない判断をしました。現在のDB管理は文書登録との原子性、APIから参照するジョブ状態、長時間再試行、停止後の回復を担い、RabbitMQ通知は処理開始を早めます。Watermillを追加してもDBのclaimと回復処理は残り、RabbitMQを正本にするとDB更新との二重書込みやSpend Capを含む再設計が必要です。判断理由と再検討条件をこのファイルに残します。

## 主な実装箇所

- [`adapters/http/src/lib.rs`](../../adapters/http/src/lib.rs)で文書・jobをPostgreSQLへ保存した後にRabbitMQへ通知します。
- [`adapters/extraction-queue/src/lib.rs`](../../adapters/extraction-queue/src/lib.rs)で`lapin`を使ったdurable quorum queueのpublish・consumeを扱います。
- [`apps/worker/src/main.rs`](../../apps/worker/src/main.rs)で通知を受け取り、job IDを指定したclaimを呼び出します。
- [`adapters/worker-runtime/src/lib.rs`](../../adapters/worker-runtime/src/lib.rs)でPostgreSQLからの通常claim、再試行、期限切れjobの回復を扱います。

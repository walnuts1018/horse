# デプロイと復元

## バックアップの構成

HorseのPostgreSQLデータは共有クラスタ`postgresql-default`に保存されます。CloudNativePGのScheduledBackupはクラスタ全体をSeaweedFSの`cloudnative-pg-backup`バケットへ保存し、WALアーカイブを使った時点復旧に対応します。現在のスケジュールはcron式`0 0 19 */7 * *`、保持期間は7日です。復旧可能な時刻はベースバックアップとWALアーカイブの状態に依存するため、障害対応時に`Backup`リソースとObjectStoreの状態を確認してください。

Horseのアップロード原本はSeaweedFSの非公開`horse`バケットに保存されます。バケットではS3 versioningを有効にします。誤上書きや削除後の復元に利用できますが、古いオブジェクトversionを期限削除するlifecycle policyは設定していないため、保存量と費用を定期的に確認してください。versioningは誤削除対策であり、SeaweedFS障害やクラスタ全損に備える独立したオフサイトコピーではありません。

## PostgreSQLからの復元

復元は既存の`postgresql-default`を変更せず、新しい一時CloudNativePGクラスタへ共有クラスタの物理バックアップを復元してからHorseデータを取り出します。既存クラスタのバックアップにはHorse以外のデータベースも含まれるため、復元先を本番クラスタへ直接切り替えないでください。

1. `databases` namespaceの`Backup`リソースで完了した最新のベースバックアップを確認し、CloudNativePGのWAL archiveとSeaweedFS `seaweedfs-store` ObjectStoreが利用可能であることを確認します。予定時刻がバックアップ保持期間を超えている場合は復元できません。
2. `databases` namespaceに一時クラスタ用の別名を付けたCloudNativePG `Cluster`を用意します。`bootstrap.recovery.source`を`seaweedfs-backup`にし、`externalClusters`のpluginに`barmanObjectName: seaweedfs-store`と元クラスタの`serverName: postgresql-default`を指定します。ストレージ、リソース、証明書、SeaweedFSのprojected service account tokenは既存クラスタと同等に設定します。
3. 一時クラスタが復旧したら、そのクラスタへ接続して`horse`データベースの存在、テーブル数、対象データの時刻を確認します。復旧クラスタにはHorse以外のデータも含まれるため、アクセスを制限し、検証後に破棄してください。
4. Horseデータを本番へ戻す場合は、復旧クラスタから`pg_dump -Fc`形式で`horse`データベースを出力します。出力ファイルをアクセス制限された作業環境へ保管し、まず別の検証用データベースへ`pg_restore`して内容を確認します。
5. 本番データベースへの復元は、Horseの書き込みを止め、現行データの退避、復元対象時刻、復旧後の整合性確認方法を決めてから実行します。既存データベースの上書きや削除を含む操作は、別途レビューされた変更手順に従ってください。

## S3原本の復元

削除または上書きしたオブジェクトは、SeaweedFS S3 APIからversioningのversion一覧を確認し、対象キーの復元可能なversionを選んでください。アプリケーションが参照するキーへ戻す前に、別キーへコピーして内容とmetadataを検証します。versioningによる復元操作は本番データを変えるため、対象キーとversion IDを二人で確認してから実行してください。

## 公開probe

Blackbox exporterは`https://horse.walnuts.dev/healthz`を30秒間隔で確認します。`target="horse"`のprobeが5分間連続で失敗すると`HorsePublicProbeFailed` alertを発火します。HorseのPodがまだ存在しないため、このalertの発火とAlertmanagerへの配送は本番環境で未検証です。

## 検証状況

この手順による復元試験はまだ実施していません。初回の障害対応に使う前に、隔離したnamespaceまたはクラスタでPostgreSQLとS3の復元を実施し、所要時間、必要な権限、復旧後のデータ検証を記録してください。

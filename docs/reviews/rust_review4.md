# Horse Rust実装の設計上の未解決事項

## 前提

2026年10月16日時点の作業ツリーを確認しました。以下は改善案であり、現行コードが不正または故障しているという主張ではありません。業務要件に照らして、採用する範囲を決める必要があります。

## Bounded Contextとテーブル所有権

BookkeepingのPostgreSQL AdapterからEvidence、Policy、Catalog、Householdが所有するテーブルを直接参照する処理が残っています。購入作成のcross-context移行だけでは解決しません。各Contextのテーブル所有者を`docs/plan.md`へ網羅的に記録し、複数Contextのデータを必要とする処理は既存のtransactionを保ったまま`contexts/cross-context/postgres`の契約を経由する必要があります。

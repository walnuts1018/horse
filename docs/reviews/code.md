# Horse コード・アーキテクチャの未解決項目

対象: [walnuts1018/horse](https://github.com/walnuts1018/horse)、`main` のコミット [`b80a58f`](https://github.com/walnuts1018/horse/commit/b80a58f8de5cc072cffc8517630ce009c97d0a93)

## 優先度の高い改善点

### 1. P2: `App`に集中している責務を分離する

対象: [`horse-app.tsx`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/app/horse-app.tsx)（認証・世帯一覧・世帯詳細取得の`240-259,267-295`行目含む）

`App`は745行あり、画面遷移、未保存変更のブロック、世帯とページの表示、通知表示を管理しています。世帯セッション、購入Outbox送信、招待受諾は`app/model`のhookへ分離済みです。

残る画面遷移、未保存変更、アプリshellの境界を確認し、変更理由を追いやすい単位へ分離してください。

行数を減らす目的だけで細分化せず、App全体の画面構成と遷移を担う境界を保ってください。

## 実施順序

| 順位 | 作業 | 主な効果 |
| -- | ----------------------- | ------------------------ |
| 1 | Appの残る責務 | 変更の追跡 |

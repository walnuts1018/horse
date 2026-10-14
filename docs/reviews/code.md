# Horse コード・アーキテクチャの未解決項目

対象: [walnuts1018/horse](https://github.com/walnuts1018/horse)、`main` のコミット [`b80a58f`](https://github.com/walnuts1018/horse/commit/b80a58f8de5cc072cffc8517630ce009c97d0a93)

## 優先度の高い改善点

### 1. P2: `App`に集中している責務を分離する

対象: [`horse-app.tsx`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/app/horse-app.tsx)（認証・世帯一覧・世帯詳細取得の`240-259,267-295`行目含む）

`App`は745行あり、画面遷移、未保存変更のブロック、世帯とページの表示、通知表示を管理しています。世帯セッション、購入Outbox送信、招待受諾は`app/model`のhookへ分離済みです。

残る画面遷移、未保存変更、アプリshellの境界を確認し、変更理由を追いやすい単位へ分離してください。

行数を減らす目的だけで細分化せず、App全体の画面構成と遷移を担う境界を保ってください。

### 2. P2: 取り込み確定処理を純粋関数と実行処理に分ける

対象: [`use-import-confirmation.ts`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/pages/imports/model/use-import-confirmation.ts)

約620行のHookが、レシート編集、支払口座の推定、金額確認、購入登録、銀行明細、返金、振替、貸付、返済などの処理を担当しています。

この部分はドメイン処理なので、無理に統合すると逆に読みにくくなります。

そこで、入力検証とMutation用payload生成を純粋関数として切り出し、Hookにはフォーム状態と非同期処理の調整だけを残すことを推奨します。例えばレシート確定と明細取引確定は別モジュールにできます。

現在のテストは補助関数中心なので、分離した金額計算・返金・複数支払の入力生成も直接テストできるようにすると改善効果が大きいです。

### 3. P2: ブラウザー離脱時の未保存確認

対象: [`horse-app.tsx:176-185`](https://github.com/walnuts1018/horse/blob/b80a58f8de5cc072cffc8517630ce009c97d0a93/web/src/app/horse-app.tsx#L176-L185)

画面遷移は`useBlocker`で確認し、ブラウザー離脱には`enableBeforeUnload`を設定しています。しかし公開環境で購入下書きから直接再読み込みした際に確認が表示されなかった記録があるため、ブラウザーでの更新・タブ終了時の動作は未解決です。

## 実施順序

| 順位 | 作業 | 主な効果 |
| -- | ----------------------- | ------------------------ |
| 1 | Appの残る責務と未保存確認 | 変更の追跡と誤離脱を防止 |
| 2 | 取り込み確定処理の分離 | 変更時の見通しを改善 |

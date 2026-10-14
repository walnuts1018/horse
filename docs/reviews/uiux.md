# UI/UX未解決レビュー

## スマートフォンの操作導線

### P1：前回の買い物をコピーした後、コピー内容が画面外になる

該当箇所：

- [purchase-create-page.module.css L233–245](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/purchase-create-page.module.css#L233-L245)
- [edit-purchase-details.tsx L152–197](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-details.tsx#L152-L197)

モバイルでは、画面が次の順番です。

`購入情報 → 品目入力・品目一覧 → 過去の購入履歴`

「前回の購入をコピー」は過去の購入履歴にあります。

ところが、コピーした品目はその上にある品目一覧へ反映されます。コピー処理自体には、コピー先の品目へスクロール・フォーカスする操作がありません。

ユーザーはコピーした後、内容を確認するために上へ戻る必要があります。

おすすめは、過去の購入からコピーする操作を品目入力の近くにも配置することです。あるいはコピー完了直後に品目一覧へ移動し、最初の編集対象を表示します。

また、現在のコピー処理は既存の入力品目を置き換えます。入力済み品目が存在する場合は、「置換」「追加」を選べるようにすると、間違った操作からの復旧も簡単になります。

## その他の未解決項目

| 優先度 | 確認できた箇所 | 推奨する改善 |
| --- | -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| P1 | [ホームのレシート導線](https://github.com/walnuts1018/horse/blob/main/web/src/pages/home/ui/home-page.tsx#L185-L201) | ホームの「レシートを取り込む」は取り込み画面への遷移なので、撮影開始までさらに操作が必要。ホームから直接撮影できる導線を追加 |
| P1 | [購入情報の編集](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-details.tsx#L80-L100) | 店名変更に「詳細を開く → 入力欄を選ぶ」の2操作が必要。概要の店名をタップすると直接編集できるようにする |
| P2 | [標準負担の表示](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-items.tsx#L695-L698) | 標準負担の見出しが全プール名の先頭1文字を並べており、実際の配分と一致するとは限らない。適用されるプール名・比率を簡潔に表示 |
| P2 | [CSV貼り付けエラー](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-items.tsx#L540-L552) | 不正行があるCSV貼り付けでブラウザ標準の`alert`/`confirm`を使用。正常な行のプレビューと不正行の修正を同一画面に表示 |

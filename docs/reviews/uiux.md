# UI/UX未解決レビュー

## 購入入力

### P1：最後に入力中の品目が、画面の合計額に含まれない

該当箇所：

- [purchase-create-page.tsx L131–132](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/purchase-create-page.tsx#L131-L132)
- [purchase-create-page.tsx L322–347](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/purchase-create-page.tsx#L322-L347)

現在、合計金額の算出対象は `itemsValue.items` のみです。

一方で、「保存して送信」を押すと、入力欄に残っている未追加の品目も `prepareForSubmit()` によって保存対象に含まれます。

例えば次の状況です。

| 状態 | 金額 |
| ---------- | ---- |
| 追加済み：牛乳 | ¥200 |
| 追加済み：パン | ¥150 |
| 入力中：ヨーグルト | ¥180 |
| 画面に表示される合計 | ¥350 |
| 実際の保存対象の合計 | ¥530 |

つまり、表示されている合計と、保存操作の結果が一致しない時間が存在します。

これは単に金額表示の問題ではなく、ユーザーが最後の品目の追加を忘れたのか、すでに追加したのか判断しづらくなる問題です。

改善案： 入力中の品目の金額も合計プレビューへ含め、未確定分を区別します。保存時には今表示している合計と同じ計算結果を使います。

決済金額の自動調整も同じプレビュー合計に連動させることで、入力中だけ金額がずれる状態をなくせます。

### P1：商品候補が出ると、Enterによる最短入力が途切れる可能性がある

該当箇所：

- [purchase-item-entry.tsx L108–146](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/purchase-item-entry.tsx#L108-L146)
- [edit-purchase-items.tsx L465–483](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-items.tsx#L465-L483)

品名入力のComboboxは`autoHighlight`を有効にしています。一方、「Enterで金額入力へ進む」処理には、`aria-activedescendant`が存在しないことという条件があります。

つまり、候補が表示されて最初の項目が自動選択状態になっていると、Enterは金額欄への移動ではなく、候補の選択に消費される可能性があります。

これは「品名 → Enter → 金額 → Enter → 次の品名」という、最も重要な連続入力導線に影響します。

改善案： 候補の自動ハイライトと、ユーザーが矢印キーなどで明示的に候補を選択した状態を区別することです。通常のEnterは金額欄へ進み、明示的に候補を選んでいる場合だけ候補確定に使います。

候補を選ぶことが任意の補助機能であり、候補が出現したことで通常入力の操作数が増えない設計にします。

### P1：決済元検索で日本語IMEのEnterが候補選択と衝突する

該当箇所：[edit-purchase-payments.tsx L49–88](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-payments.tsx#L49-L88)

決済元の検索欄では、Enterを押すと候補を選択する処理がありますが、日本語IMEの変換中かどうかを確認していません。

例えば「楽天銀行」と入力するとき、変換確定のEnterが決済元候補の選択として扱われる危険があります。

品名入力ではすでにIMEを考慮しているため、決済元検索にも同じ原則を適用します。変換中はEnter、矢印キー、EscapeによるCombobox操作を発生させないようにしてください。

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

### P1：一括商品変更が、商品数の増加に弱い

該当箇所：[ledger-item-bulk-actions.tsx L144–194](https://github.com/walnuts1018/horse/blob/main/web/src/pages/ledger/ui/ledger-item-bulk-actions.tsx#L144-L194)

一括編集の「商品」は、すべての商品バリエーションを列挙する通常の`select`です。

商品マスタが数百〜数千件になると、目的の商品を見つける操作が大変になります。

一方、通常の商品編集では検索可能な商品選択UIがすでに存在しています。

一括編集でも同じ検索基盤を利用し、品名・別名・型番・容量で探せるようにするべきです。

また、一括タグ編集にはネイティブの`select multiple`が使われています。スマホでは選択状態が分かりづらく、複数選択の方法もOSによって異なります。

こちらはチェック付き候補一覧へ変更し、「選択中のタグ」をチップで見せる方式が適切です。

## その他の未解決項目

| 優先度 | 確認できた箇所 | 推奨する改善 |
| --- | -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| P1 | [ホームのレシート導線](https://github.com/walnuts1018/horse/blob/main/web/src/pages/home/ui/home-page.tsx#L185-L201) | ホームの「レシートを取り込む」は取り込み画面への遷移なので、撮影開始までさらに操作が必要。ホームから直接撮影できる導線を追加 |
| P1 | [購入情報の編集](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-details.tsx#L80-L100) | 店名変更に「詳細を開く → 入力欄を選ぶ」の2操作が必要。概要の店名をタップすると直接編集できるようにする |
| P2 | [標準負担の表示](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-items.tsx#L695-L698) | 標準負担の見出しが全プール名の先頭1文字を並べており、実際の配分と一致するとは限らない。適用されるプール名・比率を簡潔に表示 |
| P2 | [モバイルカテゴリ選択](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/purchase-create-page.css#L655-L661) | 編集済み品目のカテゴリ選択欄が高さ23px。タップ領域を十分に広げ、ほかの操作と誤タップしない配置にする |
| P2 | [CSV貼り付けエラー](https://github.com/walnuts1018/horse/blob/main/web/src/pages/purchase-create/ui/edit-purchase-items.tsx#L540-L552) | 不正行があるCSV貼り付けでブラウザ標準の`alert`/`confirm`を使用。正常な行のプレビューと不正行の修正を同一画面に表示 |

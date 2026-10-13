## ブラウザー離脱時の未保存変更

アプリ内遷移ではdirty状態の購入フォームから離れるときに破棄確認が表示される。`beforeunload`によるタブ閉じ・更新時のブラウザー確認は公開環境で未確認のため、保存していない変更が失われないことを確認する。

## モバイルのpull-to-refresh

AndroidとiOSの実機でdrawerの開閉・スクロール中に意図しないpull-to-refreshが起きないことを確認する。CSSの`overscroll-behavior`とbody固定が対象ブラウザーで有効か確認する。

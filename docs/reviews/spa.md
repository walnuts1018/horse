## ブラウザー離脱時の未保存変更

購入画面は編集イベント内で未保存状態を離脱ガードへ同期し、IndexedDBへの下書き保存が成功した時点で解除する。アプリ内遷移と`beforeunload`の両方で、保存前に離脱しようとした場合の確認と、保存済み下書きの再読み込み後の復元をブラウザーで確認する。

## モバイルのpull-to-refresh

AndroidとiOSの実機でdrawerの開閉・スクロール中に意図しないpull-to-refreshが起きないことを確認する。CSSの`overscroll-behavior`とbody固定が対象ブラウザーで有効か確認する。

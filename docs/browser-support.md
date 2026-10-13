# ブラウザー対応

## 対象ブラウザー

フロントエンドのCSS、JavaScript、Web APIは[web/.browserslistrc](../web/.browserslistrc)に記載したブラウザーを対象にします。対象はデスクトップ版Chrome・Edge・Firefoxの直近2バージョン、Safari 26以降、Android版Chromeの直近2バージョン、iOS 26以降です。

## 対応方法

対象ブラウザーで利用できないCSS機能やWeb APIを使う場合は、実行時に機能を検出して代替手段へ切り替える段階的拡張にします。機能検出も代替手段も用意できない機能は使用しません。

oxlintの`compat/compat`とStylelintの`plugin/browser-compat`はBrowserslistの対象を参照し、JavaScript APIとCSS機能の互換性を検査します。例外は個別のlint設定で対象を絞って許可します。

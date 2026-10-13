# バックエンドライブラリの未解決候補

実装済みのライブラリ導入・不採用の判断は[レビュー対応記録](../decisions/review-responses.md)に記録しています。このファイルには適用範囲と維持する境界を記録します。

## 適用範囲と境界

| ライブラリ | 適用範囲 |
| --- | --- |
| `github.com/go-playground/validator/v10` | Worker設定と、タグ・カテゴリ・商品別名・予算・定期支出のApplication commandにある文字列の空欄・長さを検証します。GraphQLの必須値、scalar、Global IDの形式はgqlgenで検証し、金額・期間・配賦・会計整合性は各Application Serviceの業務検証に残します。 |

## 維持する境界

DB読み書きにはBob DSLを使い、生SQL文字列を直接書きません。GraphQLはgqlgen、HTTPはEcho v5、migrationはAtlasを使います。設定ファイルやCIの存在だけを確認するテスト、mock call順だけを確認するテストは追加しません。ライブラリは、実際の業務処理で責務を減らし、入力上限・認可・会計不変条件を維持できる場合に採用します。

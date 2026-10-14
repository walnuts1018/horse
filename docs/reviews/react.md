# Horse Frontend React 実装レビュー

改善余地あり

[walnuts1018/horse](https://github.com/walnuts1018/horse) の `web/src` を対象に、React Hooks、状態管理、データ取得、ルーティング、コンポーネント間のデータフローを調査しました。

結論として、不要な `useEffect` は複数あります。ただし、それ以上に改善効果が大きいのは「サーバーデータの手動管理」「Effect による状態同期」「イベントを props の変更で表現する設計」の3点です。

レビュー対象は 2026年10月14日時点の `main`、コミット [`8e1fbcac`](https://github.com/walnuts1018/horse/commit/8e1fbcacd6934f006dd9687d086b1a894a77aa72) です。ソースコードの静的レビューであり、ブラウザ上での再現テストや lint の実行は行っていません。

## 1. 重要な発見

優先度別の主な改善対象

高優先度

# 2

不整合・競合・データ損失リスク

中優先度

# 9

不要な Effect・複雑な状態管理

設計改善

# 4

データ層・ルーティング・テスト

### 高優先度

| 対象 | 問題と改善方針 |
| -------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| [購入履歴 L116–195](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/entities/purchase/api/use-household-purchases.ts#L116-L195) | 履歴の全件ロードと訂正履歴の追加取得がクライアントの Effect に依存 |
| [Relay Environment L437–550](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/shared/api/household.ts#L437-L550) | SSR 時にモジュールスコープの singleton を使用する構造。サーバーでデータ取得する場合はリクエスト間の分離が必要 |

## 2. 不要な useEffect の具体的な修正

### A. props の変更で state をリセットしている

これは最も多いアンチパターンです。

| ファイル | 行 | 現在の処理 | 推奨 |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ----------------------- | --------------------------------- |
| `horse-app.tsx` | [120](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/app/horse-app.tsx#L120) | ページ変更でメニューを閉じる | ナビゲーションイベントで閉じるか、ルートに紐付けて表示状態を管理 |
| `use-document-evidence.ts` | [33](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/imports/model/use-document-evidence.ts#L33) | 書類変更で crop state を初期化 | `key={documentId}` で編集コントローラーを再生成 |
| `use-import-confirmation.ts` | [184](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/imports/model/use-import-confirmation.ts#L184) | 書類変更でレビュー位置を初期化 | 書類単位の子コンポーネントへ分割 |
| `evidence-preview.ts` | [23](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/imports/model/evidence-preview.ts#L23) | ページ番号と入力値をリセット | keyed child と単一ページ state |
| `ledger-page.tsx` | [194](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/ledger/ui/ledger-page.tsx#L194) | 展開対象変更でハイライトを初期化 | 展開対象 ID とハイライトを対応付ける |
| `purchase-bulk-edit.ts` | [41–53](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/ledger/model/purchase-bulk-edit.ts#L41-L53) | 検索条件と世帯変更で編集 state を初期化 | selection scope による状態管理 |
| `ledger-item-bulk-edit.ts` | [42–51](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/ledger/model/ledger-item-bulk-edit.ts#L42-L51) | 同上 | 同上 |

たとえば以下の形です。

```

function DocumentReviewPage({ document }: Props) {
  return (
    <DocumentReviewEditor
      key={document.id}
      document={document}
    />
  );
}

```

これなら書類切り替え直後に古い書類の state が一瞬表示されることもありません。

ただし、`key` による再マウントは子ツリー全体を破棄するため、保存すべき編集中のデータは親または外部ストアに分離する必要があります。

### B. 派生値を Effect で通知している

該当箇所は、たとえば以下です。

| ファイル | 行 | 改善案 |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------- |
| `observation-ledger.tsx` | [36–38](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/ledger/ui/observation-ledger.tsx#L36-L38) | 親が件数データを取得・計算 |
| `manage-account-settings.tsx` | [50](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/settings/ui/manage-account-settings.tsx#L50) | pending 状態の管理を親に統合 |
| `manage-account-activity.tsx` | [40](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/settings/ui/manage-account-activity.tsx#L40) | 同上 |
| `manage-allocation-catalog.tsx` | [26](https://github.com/walnuts1018/horse/blob/8e1fbcac/web/src/pages/settings/ui/manage-allocation-catalog.tsx#L26) | 同上 |

基本原則は 子が持つ値を Effect で親へコピーしないことです。

ただし、非同期処理を子に閉じ込める設計が明確に必要なら、ライフサイクル通知自体は許容できます。無理にすべて排除する必要はありません。

## 3. データ取得と状態管理の改善

ここは新規ライブラリを導入する価値が高い領域です。

### Relay と TanStack Query の役割分担

Horse はすでに Relay を導入しています。それを捨てて TanStack Query に統一するのは推奨しません。

次のように分担するのが良いです。

| データ | 推奨技術 |
| --------------------- | ----------------------------------- |
| GraphQL の購入、世帯、カテゴリなど | Relay を継続使用 |
| GraphQL の一覧とページネーション | Relay `usePaginationFragment` |
| REST の取り込み一覧・ステータス | TanStack Query を追加 |
| REST の書類詳細・プレビュー | TanStack Query |
| URL フィルター | TanStack Router |
| 入力フォームの編集状態 | `useReducer`、必要に応じて React Hook Form |
| IndexedDB の下書き・Outbox | 専用の外部ストア、非同期永続化処理 |

TanStack Query にはキャッシュ、リクエストの共有、ポーリング、失効管理があり、現在の手動 Effect の多くを置き換えられます。

[image](https://www.google.com/s2/favicons?domain=https://tanstack.com\&sz=32)

TanStack Query React Docs

+1

### 取り込み一覧の useImportDocuments

現在は次のような責務が同居しています。

`documents` / `selected` / `nextDocumentCursor` / `loadingOlderDocuments` / `filterScopeRef` / `editedReceiptDocuments` / polling / fetch。

ここは `useInfiniteQuery` が適しています。

```

const documentsQuery = useInfiniteQuery({
  queryKey: ["documents", householdId, filters],
  initialPageParam: null as string | null,
  queryFn: async ({ pageParam, signal }) => {
    const response = await fetch(
      documentListURL(householdId, filters, pageParam),
      { signal, credentials: "include" },
    );
    if (!response.ok) throw new Error("取得失敗");
    return response.json() as Promise<DocumentPage>;
  },
  getNextPageParam: (page) =>
    page.nextCursor ?? undefined,
  refetchInterval: (query) =>
    query.state.data?.pages.some((page) =>
      page.documents.some((doc) => isProcessing(doc.status)),
    )
      ? 1200
      : false,
});

```

※ 型定義と QueryClientProvider は追加が必要です。

編集済みのレシート結果を守るため、取得キャッシュと未保存の編集内容は分離し、document ID をキーにしたローカル draft overlay をマージする構成にしてください。

### そのほかの取得処理

| 対象 | 改善内容 |
| ------------------------------- | ------------------------------------------------------------------- |
| `use-analysis-data.ts` | 4つの手動 fetch Effect を Relay または Query に統合。取得中の世代管理と error state を共通化 |
| `home-page.tsx` | 集計・精算データ取得を Query 化。現状の精算取得にはキャンセル処理がない |
| `manage-budgets.tsx` | 手動 refresh と callback ref を削減 |
| `manage-recurring-expenses.tsx` | 同上 |
| `manage-tags.tsx` | Query による一覧取得・mutation 後の失効管理 |
| `use-unmatched-observations.ts` | Relay Connection に寄せてカーソル・request generation・loading state の手動管理を削減 |
| `use-ledger-purchase-editor.ts` | 商品カタログを Relay のキャッシュから参照 |
| `product-variant-select.tsx` | 商品候補検索をキャッシュ付き debounced query に変更 |

注意点として、すべての GraphQL fetch を React Query に移すと Relay の正規化キャッシュと二重管理になります。GraphQL はまず Relay で解決してください。

## 4. React 以外の構造的な改善

### 未保存状態の登録単位

親ルートは `useBlocker` による画面遷移確認と `enableBeforeUnload` によるブラウザー終了確認を使い、重複していた `beforeunload` listener を削除しました。dirty registry は世帯とページ単位で状態を保持しますが、同一ページの複数フォームが同じ callback を共有する場合は最後の通知が状態を上書きします。複数フォームを持つページでは、各フォームの状態を集合などで集約してから親へ通知してください。

### Relay Environment の SSR 対応

`getRelayEnvironment()` はモジュールスコープの `environment` を共有しています。

ブラウザ側では問題ありませんが、TanStack Start のサーバー側でユーザー固有の GraphQL データを取得するなら、リクエストごとに Environment を生成し、サーバー間で Store を共有しない設計が必要です。

現時点で情報漏洩が起きているという意味ではありません。SSR データ取得を拡張する前に対処しておきたい構造です。

### Router の loader を活用する

現在、認証情報や世帯情報を `horse-app.tsx` の mount 後に取得しています。

TanStack Router の `beforeLoad`、`loader`、`loaderDeps` を使えば、画面遷移より前に必要なデータを読み込みやすくなります。

[image](https://www.google.com/s2/favicons?domain=https://tanstack.com\&sz=32)

TanStack Router Docs

Relay の Query preloading と組み合わせると、ページ表示後のウォーターフォール取得も減らせます。

[image](https://www.google.com/s2/favicons?domain=https://relay.dev\&sz=32)

Relay

## 5. 追加ライブラリの推奨

| ライブラリ | 推奨度 | 理由 |
| -------------------------------- | --- | ------------------------------------- |
| `@tanstack/react-query` | 高 | REST、polling、キャッシュ、race condition の管理 |
| `@tanstack/react-query-devtools` | 中 | 取得状態の開発時検証 |
| `react-hook-form` | 中 | 設定フォームや長い編集フォームの管理簡略化 |
| `@hookform/resolvers` | 中 | 既存 Zod との連携 |
| `@testing-library/react` | 高 | Effect と state の振る舞いの回帰テスト |
| `@testing-library/user-event` | 高 | Reset / Undo / Copy のイベントテスト |
| `msw` | 高 | 非同期競合、取得失敗、再取得のテスト |
| `@playwright/test` | 高 | ブラウザ履歴、下書き復元、複数タブ動作の検証 |

`zustand` は現時点では必須ではありません。親子で共有する状態を reducer と Context で十分扱えるなら、まずはそれを優先した方が設計が単純になります。

## 6. むしろ残すべき useEffect

`useEffect` 自体が悪いわけではありません。

次の実装は原則として合理的です。

| 対象 | 理由 |
| ---------------------------------------------------------- | ---------------- |
| `camera-capture.tsx` の object URL cleanup | ブラウザリソースの解放 |
| `evidence-preview.ts` の画像 Blob URL 管理 | 外部リソースとの同期 |
| `use-import-confirmation.ts` の focus / scroll | DOM の命令的操作 |
| `purchase-storage.ts` の BroadcastChannel / online listener | 外部イベント購読 |
| `use-household-purchases.ts` の GraphQL subscription | サーバーのイベントストリーム購読 |

React 公式も Effect を外部システムとの同期に使用すること自体は推奨しています。

[image](https://www.google.com/s2/favicons?domain=https://react.dev\&sz=32)

React

問題なのは、React の state や props だけで完結する変更まで Effect を介している点です。

## 7. 推奨する修正順序

1. TanStack Query を REST に導入 — imports と document preview から開始し、fetch と polling の手動管理を削減します。
2. Router に URL 状態を統合 — imports と settings の URL 同期処理を置換します。
3. React の副作用を整理 — 派生 state、親への通知、prop change reset の Effect を削除します。
4. 回帰テストの強化 — 下書き復元、連続操作、ネットワーク競合、世帯切り替え、Back/Forward を自動テストします。

今回のレビューで特に勧めたいのは、単純に `useEffect` を減らすのではなく、state の所有権を明確にして、イベント・サーバーデータ・ブラウザの外部状態を分離することです。

これを徹底すれば、不要な Effect は自然に減りますし、React 19 のレンダリングモデルとの相性も良くなります。現状の技術スタックは悪くありません。Relay、TanStack Router、Zod は活かしたまま、管理層を整理する方針が最も合理的です。

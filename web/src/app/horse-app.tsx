import { Dialog } from "@base-ui/react/dialog";
import {
  Link,
  Outlet,
  useBlocker,
  useNavigate,
  useParams,
  useRouterState,
} from "@tanstack/react-router";
import {
  ArrowLeftRight,
  AlertCircle,
  BarChart3,
  BookOpen,
  CloudUpload,
  ChevronDown,
  ChevronRight,
  FileImage,
  Home,
  LogOut,
  Menu,
  Plus,
  Settings2,
  X,
  Search,
  WifiOff,
} from "lucide-react";
import {
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
  type SubmitEvent,
} from "react";

import {
  acceptHouseholdInvitation,
  createHousehold,
  readHousehold,
  readHouseholds,
} from "../entities/household";
import { useCreateAndPostPurchase } from "../entities/purchase";
import { ApiError, getViewer, type Household } from "../shared/api";
import {
  claimPurchaseOutbox,
  completePurchaseOutbox,
  errorMessage,
  markPurchaseOutboxFailed,
  renewPurchaseOutboxLease,
  toError,
  usePurchaseOutbox,
} from "../shared/lib";
import { Notice, type NoticeKind } from "../shared/ui";
import { isAppPage, type AppPage } from "./app-page";
import { GraphQLErrorBoundary } from "./graphql-error-boundary";
import {
  createUnsavedChangesRegistry,
  hasSameNavigationTarget,
  householdRouteContext,
} from "./household-route-context";

const mobileViewportQuery = "(width <= 760px)";

function subscribeToMobileViewport(callback: () => void) {
  const media = window.matchMedia(mobileViewportQuery);
  media.addEventListener("change", callback);
  return () => media.removeEventListener("change", callback);
}

function getMobileViewport(): boolean | null {
  return window.matchMedia(mobileViewportQuery).matches;
}

function getServerMobileViewport(): null {
  return null;
}

const pendingInvitationStorageKey = "horse:pending-household-invitation";
const pendingInvitationChangeEvent = "horse:pending-household-invitation-change";

function subscribeToPendingInvitation(onStoreChange: () => void) {
  window.addEventListener(pendingInvitationChangeEvent, onStoreChange);
  window.addEventListener("storage", onStoreChange);
  return () => {
    window.removeEventListener(pendingInvitationChangeEvent, onStoreChange);
    window.removeEventListener("storage", onStoreChange);
  };
}

function getPendingInvitationSnapshot() {
  return (
    new URLSearchParams(window.location.hash.slice(1)).get("invite") ??
    window.sessionStorage.getItem(pendingInvitationStorageKey) ??
    ""
  );
}

function getServerPendingInvitationSnapshot() {
  return "";
}

function clearPendingInvitation() {
  window.sessionStorage.removeItem(pendingInvitationStorageKey);
  window.dispatchEvent(new Event(pendingInvitationChangeEvent));
}

function dismissPendingInvitation() {
  clearPendingInvitation();
}

export function App() {
  const navigate = useNavigate({ from: "/" });
  const params = useParams({ strict: false });
  const householdId = "householdId" in params ? params.householdId : undefined;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const returnTo = useRouterState({
    select: (state) => `${state.location.pathname}${state.location.searchStr}`,
  });
  const page = appPageFromPath(pathname);
  const createAndPostPurchase = useCreateAndPostPurchase();
  const [viewer, setViewer] = useState<{ id: string; displayName: string; email?: string } | null>(
    null,
  );
  const outbox = usePurchaseOutbox(viewer?.id);
  const [households, setHouseholds] = useState<Array<{ id: string; name: string }>>([]);
  const [household, setHousehold] = useState<Household | null>(null);
  const [householdLoading, setHouseholdLoading] = useState(false);
  const activeHousehold = household?.id === householdId ? household : null;
  const [unsavedChangesRegistry] = useState(createUnsavedChangesRegistry);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [mobileMenu, setMobileMenu] = useState(false);
  const isMobileViewport = useSyncExternalStore(
    subscribeToMobileViewport,
    getMobileViewport,
    getServerMobileViewport,
  );
  const [householdMenu, setHouseholdMenu] = useState(false);
  const householdMenuOpen = householdMenu && households.length > 1;
  const [notice, setNotice] = useState<{ message: string; kind: NoticeKind } | null>(null);
  const noticeTimeout = useRef<number | undefined>(undefined);
  const [loggingOut, setLoggingOut] = useState(false);
  const pendingInvitation = useSyncExternalStore(
    subscribeToPendingInvitation,
    getPendingInvitationSnapshot,
    getServerPendingInvitationSnapshot,
  );
  const [acceptingInvitation, setAcceptingInvitation] = useState(false);
  const activeSends = useRef(new Set<string>());
  const senderId = useRef("");
  const currentPage = useRef(page);
  const purchaseCreatePreviousPage = useRef<AppPage>("home");
  const currentHouseholdId = useRef(household?.id);
  const autoNavigateKey = useRef<string | null>(null);
  const appShellRef = useRef<HTMLDivElement>(null);

  // レンダー中のref書き込みは並行レンダーで壊れるため、コミット後に最新値を反映する
  useEffect(() => {
    currentPage.current = page;
  }, [page]);
  useEffect(() => {
    currentHouseholdId.current = household?.id;
  }, [household?.id]);

  const previousPage = useRef(page);
  useEffect(() => {
    if (previousPage.current === page) return;
    previousPage.current = page;
    setMobileMenu(false);
  }, [page]);

  useBlocker({
    shouldBlockFn: ({ current, next }) => {
      if (hasSameNavigationTarget(current, next)) return false;
      return (
        unsavedChangesRegistry.hasUnsavedChanges() &&
        !window.confirm("未保存の変更を破棄しますか？")
      );
    },
    enableBeforeUnload: () => unsavedChangesRegistry.hasUnsavedChanges(),
  });

  const unsavedChangesRegistrationId = `household:${householdId ?? "none"}:page:${page}`;
  const reportUnsavedChanges = useCallback(
    (unsaved: boolean) => unsavedChangesRegistry.report(unsavedChangesRegistrationId, unsaved),
    [unsavedChangesRegistrationId, unsavedChangesRegistry],
  );
  useEffect(
    () => () => unsavedChangesRegistry.clear(unsavedChangesRegistrationId),
    [unsavedChangesRegistrationId, unsavedChangesRegistry],
  );

  const navigateToHouseholdPage = useCallback(
    (id: string, nextPage: AppPage, replace = false) => {
      const toByPage = {
        home: "/households/$householdId",
        ledger: "/households/$householdId/ledger",
        new: "/households/$householdId/new",
        settlement: "/households/$householdId/settlement",
        analysis: "/households/$householdId/analysis",
        imports: "/households/$householdId/imports",
        settings: "/households/$householdId/settings",
      } as const;
      void navigate({
        to: toByPage[nextPage],
        params: { householdId: id },
        search: true,
        replace,
      });
    },
    [navigate],
  );

  const navigateToPage = useCallback(
    (nextPage: AppPage) => {
      if (page === nextPage) return;
      if (nextPage === "new") {
        if (page !== "new") purchaseCreatePreviousPage.current = page;
      }
      if (householdId) navigateToHouseholdPage(householdId, nextPage);
    },
    [page, householdId, navigateToHouseholdPage],
  );

  const refreshHousehold = useCallback(async (id: string) => {
    setHousehold(await readHousehold(id));
  }, []);

  useEffect(() => {
    const inviteToken = new URLSearchParams(window.location.hash.slice(1)).get("invite");
    if (!inviteToken) return;
    window.sessionStorage.setItem(pendingInvitationStorageKey, inviteToken);
    window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
  }, []);

  useEffect(() => {
    let active = true;
    void Promise.all([getViewer(), readHouseholds()])
      .then(([session, householdList]) => {
        if (!active) return;
        setViewer(session);
        setHouseholdMenu(false);
        setHouseholds(householdList);
      })
      .catch((reason: unknown) => {
        if (!active) return;
        const err = toError(reason);
        if (err instanceof ApiError && err.status === 401) setError("ログインして家計簿を開く");
        else setError(err.message || "データを読み込めませんでした");
      })
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, []);

  const showNotice = useCallback((message: string, kind: NoticeKind = "info") => {
    if (noticeTimeout.current !== undefined) window.clearTimeout(noticeTimeout.current);
    setNotice({ message, kind });
    noticeTimeout.current = window.setTimeout(() => setNotice(null), 3200);
  }, []);

  useEffect(() => {
    if (!households.length) return;
    const requestedHousehold = households.find((item) => item.id === householdId);
    if (!requestedHousehold) {
      const target = households[0];
      navigateToHouseholdPage(target.id, "home", true);
      return;
    }
    const target = requestedHousehold;
    if (currentHouseholdId.current === target.id) {
      setHouseholdLoading(false);
      return;
    }
    let active = true;
    setHouseholdLoading(true);
    void readHousehold(target.id)
      .then((data) => {
        if (!active) return;
        setHousehold(data);
      })
      .catch((reason: unknown) => {
        if (!active) return;
        showNotice(errorMessage(reason) || "家計簿を読み込めませんでした", "error");
        const currentId = currentHouseholdId.current;
        if (currentId && currentId !== target.id)
          navigateToHouseholdPage(currentId, currentPage.current, true);
      })
      .finally(() => {
        if (active) setHouseholdLoading(false);
      });
    return () => {
      active = false;
    };
  }, [households, householdId, navigateToHouseholdPage, showNotice]);

  const onPurchaseQueued = useCallback(
    (idempotencyKey: string) => {
      autoNavigateKey.current = idempotencyKey;
      showNotice("送信待ちに保存しました", "info");
    },
    [showNotice],
  );
  const onBackFromPurchase = useCallback(
    () => navigateToPage(purchaseCreatePreviousPage.current),
    [navigateToPage],
  );
  useEffect(() => {
    if (!viewer || !outbox.ready || !outbox.online) return;
    if (!senderId.current) senderId.current = crypto.randomUUID();
    const owner = senderId.current;
    const availableHouseholds = new Set(households.map((item) => item.id));
    for (const entry of outbox.entries) {
      const key = entry.input.idempotencyKey;
      if (
        entry.status !== "PENDING" ||
        !availableHouseholds.has(entry.input.householdId) ||
        activeSends.current.has(key)
      )
        continue;
      activeSends.current.add(key);
      void (async () => {
        let claimed = false;
        let leaseRenewal: number | undefined;
        try {
          const request = await claimPurchaseOutbox(viewer.id, entry.input.householdId, key, owner);
          if (!request) return;
          claimed = true;
          leaseRenewal = window.setInterval(() => {
            void renewPurchaseOutboxLease(viewer.id, entry.input.householdId, key, owner).catch(
              () => {},
            );
          }, 10_000);
          await createAndPostPurchase(request.input);
          await completePurchaseOutbox(viewer.id, entry.input.householdId, key, owner);
          if (currentHouseholdId.current === request.input.householdId) {
            await refreshHousehold(request.input.householdId);
            showNotice("買い物を帳簿に追加しました", "success");
          }
          if (autoNavigateKey.current === key && currentPage.current === "new") {
            autoNavigateKey.current = null;
            navigateToPage("ledger");
          }
        } catch (reason) {
          if (claimed) {
            const message =
              reason instanceof ApiError
                ? reason.message
                : reason instanceof TypeError ||
                    (reason instanceof DOMException &&
                      ["AbortError", "NetworkError"].includes(reason.name))
                  ? "接続を確認してから再送してください"
                  : "買い物を送信できませんでした。再送または破棄してください";
            await markPurchaseOutboxFailed(
              viewer.id,
              entry.input.householdId,
              key,
              owner,
              message,
            ).catch(() => {});
          }
        } finally {
          if (leaseRenewal !== undefined) window.clearInterval(leaseRenewal);
          activeSends.current.delete(key);
          if (claimed) await outbox.refresh();
        }
      })();
    }
  }, [
    viewer,
    households,
    outbox,
    createAndPostPurchase,
    navigateToPage,
    refreshHousehold,
    showNotice,
  ]);
  const currentOutbox = outbox.entries.filter(
    (entry) => entry.input.householdId === activeHousehold?.id,
  );
  const failedOutboxCount = currentOutbox.filter((entry) => entry.status === "FAILED").length;
  const pendingOutboxCount = currentOutbox.filter((entry) => entry.status === "PENDING").length;
  const syncLabel = !outbox.online
    ? "オフライン"
    : failedOutboxCount > 0
      ? `送信失敗 ${failedOutboxCount}件`
      : pendingOutboxCount > 0
        ? `送信待ち ${pendingOutboxCount}件`
        : "";
  const logout = async () => {
    const dirtyIds = unsavedChangesRegistry.getDirtyIds();
    const hadUnsavedChanges = dirtyIds.length > 0;
    if (hadUnsavedChanges && !window.confirm("未保存の変更を破棄してログアウトしますか？")) return;
    if (hadUnsavedChanges) unsavedChangesRegistry.clearAll();
    setLoggingOut(true);
    try {
      const response = await fetch("/auth/logout", {
        method: "POST",
        credentials: "include",
      });
      if (!response.ok) throw new Error("ログアウトできませんでした");
      unsavedChangesRegistry.clearAll();
      window.location.reload();
    } catch (reason) {
      for (const id of dirtyIds) unsavedChangesRegistry.report(id, true);
      showNotice(errorMessage(reason), "error");
      setLoggingOut(false);
    }
  };
  const acceptInvitation = async () => {
    if (!pendingInvitation || acceptingInvitation) return;
    const dirtyIds = unsavedChangesRegistry.getDirtyIds();
    const hadUnsavedChanges = dirtyIds.length > 0;
    if (hadUnsavedChanges && !window.confirm("未保存の変更を破棄して招待を受け入れますか？"))
      return;
    if (hadUnsavedChanges) unsavedChangesRegistry.clearAll();
    setAcceptingInvitation(true);
    try {
      const result = await acceptHouseholdInvitation({ token: pendingInvitation });
      clearPendingInvitation();
      setHouseholdMenu(false);
      setHouseholds((current) => [
        { id: result.id, name: result.name },
        ...current.filter((item) => item.id !== result.id),
      ]);
      navigateToHouseholdPage(result.id, page);
      showNotice("家計簿に参加しました", "success");
    } catch (reason) {
      for (const id of dirtyIds) unsavedChangesRegistry.report(id, true);
      showNotice(errorMessage(reason), "error");
    } finally {
      setAcceptingInvitation(false);
    }
  };
  const selectHousehold = (id: string) => {
    if (!households.some((item) => item.id === id)) return;
    if (id === activeHousehold?.id) return;
    if (id === householdId) {
      // 同じ家計簿を選び直したときは読み込みをやり直す
      setHouseholdLoading(true);
      void readHousehold(id)
        .then((data) => setHousehold(data))
        .catch((reason: unknown) => {
          showNotice(errorMessage(reason) || "家計簿を読み込めませんでした", "error");
        })
        .finally(() => setHouseholdLoading(false));
      return;
    }
    navigateToHouseholdPage(id, page);
  };

  const onNavigateToPurchase = useCallback(() => {
    if (page !== "new") purchaseCreatePreviousPage.current = page;
  }, [page]);
  const routeContext = useMemo(
    () => ({
      household: activeHousehold,
      viewerId: viewer?.id ?? null,
      navigateToPage,
      onNavigateToPurchase,
      reportUnsavedChanges,
      onPurchaseQueued,
      onBackFromPurchase,
      refreshHousehold,
      showNotice,
    }),
    [
      activeHousehold,
      viewer?.id,
      navigateToPage,
      onNavigateToPurchase,
      reportUnsavedChanges,
      onPurchaseQueued,
      onBackFromPurchase,
      refreshHousehold,
      showNotice,
    ],
  );

  if (loading)
    return (
      <main className="loading-screen">
        <div className="brand-mark">h</div>
        <span className="loading-dot" /> 家計簿を開いています
      </main>
    );
  if (error || !viewer)
    return (
      <main className="auth-screen">
        <div className="auth-card">
          <div className="brand-mark">h</div>
          <h1>家計簿</h1>
          <a
            className="button button-primary button-wide"
            href={`/auth/login?returnTo=${encodeURIComponent(returnTo)}`}
          >
            ログイン <ChevronRight size={18} />
          </a>
          {pendingInvitation && <p>ログイン後に招待を確認できます</p>}
          {error && error !== "ログインして家計簿を開く" && (
            <p className="inline-error" role="alert">
              {error}
            </p>
          )}
        </div>
      </main>
    );

  return (
    <Dialog.Root
      open={isMobileViewport === false || (isMobileViewport === true && mobileMenu)}
      modal={isMobileViewport === true}
      onOpenChange={(open) => setMobileMenu(open)}
    >
      <div className="app-shell" ref={appShellRef}>
        <a className="skip-link" href="#main-content">
          メインコンテンツへ移動
        </a>
        <Dialog.Portal container={appShellRef}>
          <Dialog.Backdrop className="sidebar-scrim" />
          <Dialog.Popup
            className={`sidebar ${mobileMenu ? "sidebar-open" : ""}`}
            id="primary-navigation"
            aria-label="メインメニュー"
            initialFocus={isMobileViewport === true}
            render={<aside />}
          >
            <div className="sidebar-top">
              <Link
                className="brand"
                to="/households/$householdId"
                params={{ householdId: householdId ?? "" }}
                search
              >
                <span className="brand-mark">h</span>
                <span>
                  horse<small>HOUSEHOLD</small>
                </span>
              </Link>
              <Dialog.Close
                className="icon-button close-menu"
                render={<button type="button" aria-label="メニューを閉じる" />}
              >
                <X size={19} />
              </Dialog.Close>
            </div>
            <button
              className="household-switch"
              type="button"
              disabled={households.length <= 1}
              onClick={() => {
                if (households.length > 1) setHouseholdMenu((open) => !open);
              }}
              aria-expanded={householdMenuOpen}
              aria-controls={households.length > 1 ? "household-menu" : undefined}
            >
              <span className="household-avatar">{activeHousehold?.name.slice(0, 1) ?? "家"}</span>
              <div className="household-name">
                <b>{activeHousehold?.name ?? "家計簿"}</b>
                <small>{activeHousehold?.members.length ?? 0}人で共有</small>
              </div>
              <ChevronDown size={15} />
            </button>
            {householdMenuOpen && (
              <div className="household-menu" id="household-menu">
                {households.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => {
                      selectHousehold(item.id);
                      setHouseholdMenu(false);
                    }}
                  >
                    <span className="household-avatar">{item.name.slice(0, 1)}</span>
                    {item.name}
                  </button>
                ))}
              </div>
            )}
            <p className="nav-label">メニュー</p>
            <nav className="main-nav">
              <NavItem
                icon={<Home />}
                label="ホーム"
                active={page === "home"}
                to="/households/$householdId"
                householdId={householdId ?? ""}
              />
              <NavItem
                icon={<BookOpen />}
                label="帳簿"
                active={page === "ledger"}
                to="/households/$householdId/ledger"
                householdId={householdId ?? ""}
              />
              <NavItem
                icon={<ArrowLeftRight />}
                label="精算"
                active={page === "settlement"}
                to="/households/$householdId/settlement"
                householdId={householdId ?? ""}
              />
              <NavItem
                icon={<BarChart3 />}
                label="分析"
                active={page === "analysis"}
                to="/households/$householdId/analysis"
                householdId={householdId ?? ""}
              />
              <NavItem
                icon={<FileImage />}
                label="取り込み"
                active={page === "imports"}
                to="/households/$householdId/imports"
                householdId={householdId ?? ""}
              />
            </nav>
            <div className="sidebar-bottom">
              <div className="shared-status">
                <span>メンバー</span>
                <span className="shared-avatars">
                  {activeHousehold?.members.slice(0, 3).map((member) => (
                    <i key={member.id}>{member.displayName.slice(0, 1)}</i>
                  ))}
                </span>
              </div>
              <NavItem
                icon={<Settings2 />}
                label="設定"
                active={page === "settings"}
                to="/households/$householdId/settings"
                householdId={householdId ?? ""}
              />
              <div className="profile-row">
                <div className="profile-avatar">{viewer.displayName.slice(0, 1)}</div>
                <div>
                  <b>{viewer.displayName}</b>
                  <small>{viewer.email ?? "メンバー"}</small>
                </div>
                <button
                  className="icon-button"
                  type="button"
                  aria-label="ログアウト"
                  disabled={loggingOut}
                  onClick={() => void logout()}
                >
                  <LogOut size={16} />
                </button>
              </div>
            </div>
          </Dialog.Popup>
        </Dialog.Portal>
        <main className="main-area" id="main-content" tabIndex={-1}>
          <header className="topbar">
            <Dialog.Trigger
              className="icon-button menu-toggle"
              aria-expanded={mobileMenu}
              aria-controls="primary-navigation"
              render={<button type="button" aria-label="メニューを開く" />}
            >
              <Menu size={20} />
            </Dialog.Trigger>
            <div className="crumb">
              <span>{activeHousehold?.name ?? "家計簿"}</span>
              <ChevronRight size={14} />
              <b>{pageTitle(page)}</b>
            </div>
            <div className="topbar-actions">
              {syncLabel && (
                <Link
                  className={`sync-status-pill ${failedOutboxCount ? "sync-status-error" : ""}`}
                  to="/households/$householdId/new"
                  params={{ householdId: householdId ?? "" }}
                  search
                  onClick={(event) => {
                    if (!household) {
                      event.preventDefault();
                      return;
                    }
                    if (page !== "new") purchaseCreatePreviousPage.current = page;
                  }}
                  aria-disabled={!household}
                  tabIndex={household ? undefined : -1}
                >
                  {!outbox.online ? (
                    <WifiOff size={15} />
                  ) : failedOutboxCount ? (
                    <AlertCircle size={15} />
                  ) : (
                    <CloudUpload size={15} />
                  )}
                  <span>{syncLabel}</span>
                </Link>
              )}
              <Link
                className="search-pill"
                to="/households/$householdId/ledger"
                params={{ householdId: householdId ?? "" }}
                search
              >
                <Search size={16} />
                <span>帳簿を開く</span>
              </Link>
            </div>
          </header>
          {outbox.reloadMessage && (
            <div className="database-upgrade-notice" role="alert">
              <span>{outbox.reloadMessage}</span>
              <button type="button" onClick={() => window.location.reload()}>
                再読み込み
              </button>
            </div>
          )}
          {pendingInvitation && (
            <section className="invitation-prompt" aria-label="家計簿への招待">
              <span>家計簿への招待があります</span>
              <button
                className="button button-primary"
                type="button"
                disabled={acceptingInvitation}
                onClick={() => void acceptInvitation()}
              >
                {acceptingInvitation ? "参加中" : "招待を受け入れる"}
              </button>
              <button className="text-button" type="button" onClick={dismissPendingInvitation}>
                閉じる
              </button>
            </section>
          )}
          <div className="page-content" key={householdId ?? "no-household"}>
            <householdRouteContext.Provider value={routeContext}>
              <GraphQLErrorBoundary key={pathname}>
                <Suspense
                  fallback={<div className="loading-screen">購入履歴を読み込んでいます</div>}
                >
                  {householdLoading ? (
                    <div className="loading-screen">
                      <span className="loading-dot" /> 家計簿を開いています
                    </div>
                  ) : activeHousehold ? (
                    <Outlet />
                  ) : (
                    <EmptyHousehold
                      households={households}
                      onSelect={selectHousehold}
                      onCreated={(id) => {
                        void refreshHousehold(id);
                        setHouseholdMenu(false);
                        setHouseholds((items) => [{ id, name: "家計簿" }, ...items]);
                      }}
                    />
                  )}
                </Suspense>
              </GraphQLErrorBoundary>
            </householdRouteContext.Provider>
          </div>
        </main>
        {notice && <Notice message={notice.message} kind={notice.kind} />}
        <nav className="mobile-dock" aria-label="モバイルメニュー">
          <Link
            className={page === "home" ? "dock-active" : ""}
            aria-current={page === "home" ? "page" : undefined}
            to="/households/$householdId"
            params={{ householdId: householdId ?? "" }}
            search
          >
            <Home />
            <span>ホーム</span>
          </Link>
          <Link
            className={page === "ledger" ? "dock-active" : ""}
            aria-current={page === "ledger" ? "page" : undefined}
            to="/households/$householdId/ledger"
            params={{ householdId: householdId ?? "" }}
            search
          >
            <BookOpen />
            <span>帳簿</span>
          </Link>
          <Link
            className="dock-add"
            to="/households/$householdId/new"
            params={{ householdId: householdId ?? "" }}
            search
            onClick={() => {
              if (page !== "new") purchaseCreatePreviousPage.current = page;
            }}
          >
            <Plus />
            <span>追加</span>
          </Link>
          <Link
            className={page === "settlement" ? "dock-active" : ""}
            aria-current={page === "settlement" ? "page" : undefined}
            to="/households/$householdId/settlement"
            params={{ householdId: householdId ?? "" }}
            search
          >
            <ArrowLeftRight />
            <span>精算</span>
          </Link>
          <Link
            className={page === "analysis" ? "dock-active" : ""}
            aria-current={page === "analysis" ? "page" : undefined}
            to="/households/$householdId/analysis"
            params={{ householdId: householdId ?? "" }}
            search
          >
            <BarChart3 />
            <span>分析</span>
          </Link>
        </nav>
      </div>
    </Dialog.Root>
  );
}

function NavItem({
  icon,
  label,
  active,
  to,
  householdId,
}: {
  icon: ReactNode;
  label: string;
  active: boolean;
  to:
    | "/households/$householdId"
    | "/households/$householdId/ledger"
    | "/households/$householdId/settlement"
    | "/households/$householdId/analysis"
    | "/households/$householdId/imports"
    | "/households/$householdId/settings";
  householdId: string;
}) {
  return (
    <Link
      className={`nav-item ${active ? "nav-active" : ""}`}
      to={to}
      params={{ householdId }}
      search
      aria-current={active ? "page" : undefined}
    >
      {icon}
      <span>{label}</span>
      {active && <i />}
    </Link>
  );
}

function pageTitle(page: AppPage) {
  return (
    {
      home: "ホーム",
      ledger: "帳簿",
      new: "買い物を追加",
      settlement: "精算",
      analysis: "分析",
      imports: "取り込み",
      settings: "設定",
    } as Record<AppPage, string>
  )[page];
}

function appPageFromPath(pathname: string): AppPage {
  const child = /^\/households\/[^/]+\/([^/]+)\/?$/.exec(pathname)?.[1];
  return isAppPage(child) ? child : "home";
}

function EmptyHousehold({
  households,
  onSelect,
  onCreated,
}: {
  households: Array<{ id: string; name: string }>;
  onSelect: (id: string) => void;
  onCreated: (id: string) => void;
}) {
  const [name, setName] = useState("わが家");
  const [pending, setPending] = useState(false);
  const create = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    setPending(true);
    try {
      const household = await createHousehold({ name, memberNames: [] });
      onCreated(household.id);
    } catch (reason) {
      window.alert(errorMessage(reason));
    } finally {
      setPending(false);
    }
  };
  return (
    <section className="welcome-panel">
      <span className="eyebrow">はじめましょう</span>
      <h1>家計簿を選ぶ</h1>
      {households.length > 0 && (
        <div className="household-options">
          {households.map((item) => (
            <button
              key={item.id}
              className="household-option"
              type="button"
              onClick={() => onSelect(item.id)}
            >
              <span className="household-avatar">{item.name.slice(0, 1)}</span>
              <b>{item.name}</b>
              <ChevronRight />
            </button>
          ))}
        </div>
      )}
      <form className="create-household" onSubmit={create}>
        <label>
          新しい家計簿名
          <input value={name} onChange={(e) => setName(e.target.value)} required />
        </label>
        <button className="button button-primary" type="submit" disabled={pending}>
          家計簿を作成 <Plus size={16} />
        </button>
      </form>
    </section>
  );
}

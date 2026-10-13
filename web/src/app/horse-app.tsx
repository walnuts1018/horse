import {
  Suspense,
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { useBlocker, useNavigate, useSearch } from "@tanstack/react-router";
import {
  ArrowLeftRight,
  AlertCircle,
  BarChart3,
  BookOpen,
  Check,
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
import { ApiError, executeGraphql, getViewer, type Household } from "../shared/api";
import {
  acceptHouseholdInvitationMutation,
  createHouseholdMutation,
  householdQuery,
  householdsQuery,
} from "./api/operations";
import { useCreateAndPostPurchase } from "../entities/purchase";
import { GraphQLErrorBoundary } from "./graphql-error-boundary";
import {
  claimPurchaseOutbox,
  completePurchaseOutbox,
  markPurchaseOutboxFailed,
  renewPurchaseOutboxLease,
  usePurchaseOutbox,
} from "../shared/lib";
import { AnalysisPage } from "../pages/analysis";
import { HomePage } from "../pages/home";
import { ImportsPage } from "../pages/imports";
import { LedgerPage } from "../pages/ledger";
import { PurchaseCreatePage } from "../pages/purchase-create";
import { SettingsPage } from "../pages/settings";
import { SettlementPage } from "../pages/settlement";
import type { AppPage } from "./app-page";

async function read<T>(
  operation: Parameters<typeof executeGraphql>[0],
  variables: Record<string, unknown> = {},
) {
  return executeGraphql<T, Record<string, unknown>>(operation, variables);
}

export function App() {
  const navigate = useNavigate({ from: "/" });
  const { page: selectedPage } = useSearch({ from: "/" });
  const page: AppPage = selectedPage ?? "home";
  const outbox = usePurchaseOutbox();
  const createAndPostPurchase = useCreateAndPostPurchase();
  const [viewer, setViewer] = useState<{ id: string; displayName: string; email?: string } | null>(
    null,
  );
  const [households, setHouseholds] = useState<Array<{ id: string; name: string }>>([]);
  const [household, setHousehold] = useState<Household | null>(null);
  const hasUnsavedChangesRef = useRef(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [mobileMenu, setMobileMenu] = useState(false);
  const [householdMenu, setHouseholdMenu] = useState(false);
  const [notice, setNotice] = useState("");
  const [loggingOut, setLoggingOut] = useState(false);
  const [pendingInvitation, setPendingInvitation] = useState("");
  const [acceptingInvitation, setAcceptingInvitation] = useState(false);
  const activeSends = useRef(new Set<string>());
  const senderId = useRef("");
  const currentPage = useRef(page);
  currentPage.current = page;
  const purchaseCreatePreviousPage = useRef<AppPage>("home");
  const currentHouseholdId = useRef(household?.id);
  currentHouseholdId.current = household?.id;
  const autoNavigateKey = useRef<string | null>(null);

  useEffect(() => {
    setMobileMenu(false);
  }, [page]);

  useEffect(() => {
    const root = document.documentElement;
    if (!mobileMenu) return;
    root.classList.add("mobile-drawer-open");
    return () => root.classList.remove("mobile-drawer-open");
  }, [mobileMenu]);

  useBlocker({
    shouldBlockFn: () =>
      hasUnsavedChangesRef.current && !window.confirm("未保存の変更を破棄しますか？"),
    enableBeforeUnload: () => hasUnsavedChangesRef.current,
  });

  const reportUnsavedChanges = useCallback((unsaved: boolean) => {
    hasUnsavedChangesRef.current = unsaved;
  }, []);

  const navigateToPage = (nextPage: AppPage) => {
    if (page === nextPage) return;
    if (nextPage === "new") {
      if (page !== "new") purchaseCreatePreviousPage.current = page;
    }
    void navigate({
      search: (previous) => ({ ...previous, page: nextPage === "home" ? undefined : nextPage }),
    });
  };

  const refreshHousehold = async (id: string) => {
    const data = await read<{ household: Household }>(householdQuery, { id });
    setHousehold(data.household);
  };

  useEffect(() => {
    const key = "horse:pending-household-invitation";
    const fragment = window.location.hash.slice(1);
    const inviteToken = new URLSearchParams(fragment).get("invite");
    if (inviteToken) {
      window.sessionStorage.setItem(key, inviteToken);
      window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
      setPendingInvitation(inviteToken);
    } else {
      setPendingInvitation(window.sessionStorage.getItem(key) ?? "");
    }
  }, []);

  useEffect(() => {
    let active = true;
    void Promise.all([
      getViewer(),
      read<{ households: Array<{ id: string; name: string }> }>(householdsQuery),
    ])
      .then(async ([session, result]) => {
        if (!active) return;
        setViewer(session);
        setHouseholds(result.households);
        if (result.households[0]) {
          const data = await read<{ household: Household }>(householdQuery, {
            id: result.households[0].id,
          });
          if (active) setHousehold(data.household);
        }
      })
      .catch((reason: unknown) => {
        if (!active) return;
        const err = reason as Error;
        if (err instanceof ApiError && err.status === 401) setError("ログインして家計簿を開く");
        else setError(err.message || "データを読み込めませんでした");
      })
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, []);

  const showNotice = (message: string) => {
    setNotice(message);
    window.setTimeout(() => setNotice(""), 3200);
  };
  const showNoticeRef = useRef(showNotice);
  showNoticeRef.current = showNotice;
  const refreshHouseholdRef = useRef(refreshHousehold);
  refreshHouseholdRef.current = refreshHousehold;
  useEffect(() => {
    if (!viewer || !outbox.ready || outbox.online !== true) return;
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
          const request = await claimPurchaseOutbox(key, owner);
          if (!request) return;
          claimed = true;
          leaseRenewal = window.setInterval(() => {
            void renewPurchaseOutboxLease(key, owner).catch(() => {});
          }, 10_000);
          await createAndPostPurchase(request.input);
          await completePurchaseOutbox(key, owner);
          if (currentHouseholdId.current === request.input.householdId) {
            await refreshHouseholdRef.current(request.input.householdId);
            showNoticeRef.current("買い物を帳簿に追加しました");
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
            await markPurchaseOutboxFailed(key, owner, message).catch(() => {});
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
    outbox.entries,
    outbox.ready,
    outbox.online,
    outbox.refresh,
    createAndPostPurchase,
  ]);
  const currentOutbox = outbox.entries.filter((entry) => entry.input.householdId === household?.id);
  const failedOutboxCount = currentOutbox.filter((entry) => entry.status === "FAILED").length;
  const pendingOutboxCount = currentOutbox.filter((entry) => entry.status === "PENDING").length;
  const syncLabel =
    outbox.online === false
      ? "オフライン"
      : failedOutboxCount > 0
        ? `送信失敗 ${failedOutboxCount}件`
        : pendingOutboxCount > 0
          ? `送信待ち ${pendingOutboxCount}件`
          : "";
  const logout = async () => {
    const hadUnsavedChanges = hasUnsavedChangesRef.current;
    if (
      hadUnsavedChanges &&
      !window.confirm("未保存の変更を破棄してログアウトしますか？")
    )
      return;
    if (hadUnsavedChanges) hasUnsavedChangesRef.current = false;
    setLoggingOut(true);
    try {
      const response = await fetch("/auth/logout", {
        method: "POST",
        credentials: "include",
      });
      if (!response.ok) throw new Error("ログアウトできませんでした");
      hasUnsavedChangesRef.current = false;
      window.location.reload();
    } catch (reason) {
      if (hadUnsavedChanges) hasUnsavedChangesRef.current = true;
      showNotice((reason as Error).message);
      setLoggingOut(false);
    }
  };
  const acceptInvitation = async () => {
    if (!pendingInvitation || acceptingInvitation) return;
    if (
      hasUnsavedChangesRef.current &&
      !window.confirm("未保存の変更を破棄して招待を受け入れますか？")
    )
      return;
    setAcceptingInvitation(true);
    try {
      const result = await read<{ acceptHouseholdInvitation: { id: string; name: string } }>(
        acceptHouseholdInvitationMutation,
        { input: { token: pendingInvitation } },
      );
      window.sessionStorage.removeItem("horse:pending-household-invitation");
      setPendingInvitation("");
      await refreshHousehold(result.acceptHouseholdInvitation.id);
      setHouseholds((current) => [
        { id: result.acceptHouseholdInvitation.id, name: result.acceptHouseholdInvitation.name },
        ...current.filter((item) => item.id !== result.acceptHouseholdInvitation.id),
      ]);
      showNotice("家計簿に参加しました");
    } catch (reason) {
      showNotice((reason as Error).message);
    } finally {
      setAcceptingInvitation(false);
    }
  };
  const dismissInvitation = () => {
    window.sessionStorage.removeItem("horse:pending-household-invitation");
    setPendingInvitation("");
  };
  const selectHousehold = async (id: string) => {
    if (id === household?.id) return;
    if (
      hasUnsavedChangesRef.current &&
      !window.confirm("未保存の変更を破棄して家計簿を切り替えますか？")
    )
      return;
    try {
      await refreshHousehold(id);
      setHouseholds((current) => {
        const selected = current.find((item) => item.id === id);
        return selected ? [selected, ...current.filter((item) => item.id !== id)] : current;
      });
    } catch (reason) {
      showNotice((reason as Error).message);
    }
  };

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
          <a className="button button-primary button-wide" href="/auth/login">
            ログイン <ChevronRight size={18} />
          </a>
          {pendingInvitation && <p>ログイン後に招待を確認できます</p>}
          {error && error !== "ログインして家計簿を開く" && <p className="inline-error">{error}</p>}
        </div>
      </main>
    );

  return (
    <div className="app-shell">
      <aside className={`sidebar ${mobileMenu ? "sidebar-open" : ""}`}>
        <div className="sidebar-top">
          <a
            className="brand"
            href="#home"
            onClick={(e) => {
              e.preventDefault();
              navigateToPage("home");
            }}
          >
            <span className="brand-mark">h</span>
            <span>
              horse<small>HOUSEHOLD</small>
            </span>
          </a>
          <button
            className="icon-button close-menu"
            aria-label="メニューを閉じる"
            onClick={() => setMobileMenu(false)}
          >
            <X size={19} />
          </button>
        </div>
        <button
          className="household-switch"
          type="button"
          onClick={() => setHouseholdMenu((open) => !open)}
        >
          <span className="household-avatar">{household?.name?.slice(0, 1) ?? "家"}</span>
          <div className="household-name">
            <b>{household?.name ?? "家計簿"}</b>
            <small>{household?.members?.length ?? 0}人で共有</small>
          </div>
          <ChevronDown size={15} />
        </button>
        {householdMenu && households.length > 1 && (
          <div className="household-menu">
            {households.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => {
                  void selectHousehold(item.id);
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
            onClick={() => {
              navigateToPage("home");
            }}
          />
          <NavItem
            icon={<BookOpen />}
            label="帳簿"
            active={page === "ledger"}
            onClick={() => {
              navigateToPage("ledger");
            }}
          />
          <NavItem
            icon={<ArrowLeftRight />}
            label="精算"
            active={page === "settlement"}
            onClick={() => {
              navigateToPage("settlement");
            }}
          />
          <NavItem
            icon={<BarChart3 />}
            label="分析"
            active={page === "analysis"}
            onClick={() => {
              navigateToPage("analysis");
            }}
          />
          <NavItem
            icon={<FileImage />}
            label="取り込み"
            active={page === "imports"}
            onClick={() => {
              navigateToPage("imports");
            }}
          />
        </nav>
        <div className="sidebar-bottom">
          <div className="shared-status">
            <span>メンバー</span>
            <span className="shared-avatars">
              {household?.members?.slice(0, 3).map((member) => (
                <i key={member.id}>{member.displayName.slice(0, 1)}</i>
              ))}
            </span>
          </div>
          <NavItem
            icon={<Settings2 />}
            label="設定"
            active={page === "settings"}
            onClick={() => {
              navigateToPage("settings");
            }}
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
      </aside>
      {mobileMenu && (
        <button
          className="sidebar-scrim"
          aria-label="メニューを閉じる"
          onClick={() => setMobileMenu(false)}
        />
      )}
      <main className="main-area">
        <header className="topbar">
          <button
            className="icon-button menu-toggle"
            aria-label="メニューを開く"
            onClick={() => setMobileMenu(true)}
          >
            <Menu size={20} />
          </button>
          <div className="crumb">
            <span>{household?.name ?? "家計簿"}</span>
            <ChevronRight size={14} />
            <b>{pageTitle(page)}</b>
          </div>
          <div className="topbar-actions">
            {syncLabel && (
              <button
                className={`sync-status-pill ${failedOutboxCount ? "sync-status-error" : ""}`}
                type="button"
                onClick={() => navigateToPage("new")}
                disabled={!household}
              >
                {outbox.online === false ? (
                  <WifiOff size={15} />
                ) : failedOutboxCount ? (
                  <AlertCircle size={15} />
                ) : (
                  <CloudUpload size={15} />
                )}
                <span>{syncLabel}</span>
              </button>
            )}
            <button className="search-pill" onClick={() => navigateToPage("ledger")}>
              <Search size={16} />
              <span>帳簿を開く</span>
            </button>
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
          <section className="invitation-prompt" role="region" aria-label="家計簿への招待">
            <span>家計簿への招待があります</span>
            <button
              className="button button-primary"
              type="button"
              disabled={acceptingInvitation}
              onClick={() => void acceptInvitation()}
            >
              {acceptingInvitation ? "参加中" : "招待を受け入れる"}
            </button>
            <button className="text-button" type="button" onClick={dismissInvitation}>
              閉じる
            </button>
          </section>
        )}
        <div className="page-content" key={household?.id ?? "no-household"}>
          <GraphQLErrorBoundary>
            <Suspense fallback={<div className="loading-screen">購入履歴を読み込んでいます</div>}>
              {page === "home" && household && (
                <HomePage household={household} onNavigate={navigateToPage} />
              )}
              {page === "new" && household && (
                <PurchaseCreatePage
                  household={household}
                  onBack={() => navigateToPage(purchaseCreatePreviousPage.current)}
                  onUnsavedChanges={reportUnsavedChanges}
                  onQueued={(idempotencyKey) => {
                    autoNavigateKey.current = idempotencyKey;
                    showNotice("送信待ちに保存しました");
                  }}
                />
              )}
              {page === "ledger" && household && (
                <LedgerPage
                  household={household}
                  onAdd={() => navigateToPage("new")}
                  onUnsavedChanges={reportUnsavedChanges}
                />
              )}
              {page === "settlement" && household && (
                <SettlementPage household={household} onUnsavedChanges={reportUnsavedChanges} />
              )}
              {page === "analysis" && household && <AnalysisPage household={household} />}
              {page === "imports" && household && (
                <ImportsPage
                  household={household}
                  onUnsavedChanges={reportUnsavedChanges}
                  onAdded={async (keepOpen) => {
                    await refreshHousehold(household.id);
                    if (!keepOpen) navigateToPage("ledger");
                    showNotice("買い物を帳簿に追加しました");
                  }}
                />
              )}
              {page === "settings" && household && (
                <SettingsPage
                  household={household}
                  isOwner={household.members.some(
                    (member) => member.id === viewer.id && member.role === "OWNER",
                  )}
                  onUnsavedChanges={reportUnsavedChanges}
                  onRefresh={() => refreshHousehold(household.id)}
                  onNotice={showNotice}
                />
              )}
              {!household && (
                <EmptyHousehold
                  households={households}
                  onSelect={selectHousehold}
                  onCreated={(id) => {
                    void refreshHousehold(id);
                    setHouseholds((items) => [{ id, name: "家計簿" }, ...items]);
                  }}
                />
              )}
            </Suspense>
          </GraphQLErrorBoundary>
        </div>
      </main>
      {notice && (
        <div className="toast">
          <Check size={17} />
          {notice}
        </div>
      )}
      <nav className="mobile-dock" aria-label="モバイルメニュー">
        <button
          className={page === "home" ? "dock-active" : ""}
          onClick={() => navigateToPage("home")}
        >
          <Home />
          <span>ホーム</span>
        </button>
        <button
          className={page === "ledger" ? "dock-active" : ""}
          onClick={() => navigateToPage("ledger")}
        >
          <BookOpen />
          <span>帳簿</span>
        </button>
        <button className="dock-add" onClick={() => navigateToPage("new")}>
          <Plus />
          <span>追加</span>
        </button>
        <button
          className={page === "settlement" ? "dock-active" : ""}
          onClick={() => navigateToPage("settlement")}
        >
          <ArrowLeftRight />
          <span>精算</span>
        </button>
        <button
          className={page === "analysis" ? "dock-active" : ""}
          onClick={() => navigateToPage("analysis")}
        >
          <BarChart3 />
          <span>分析</span>
        </button>
      </nav>
    </div>
  );
}

function NavItem({
  icon,
  label,
  active,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button className={`nav-item ${active ? "nav-active" : ""}`} onClick={onClick}>
      {icon}
      <span>{label}</span>
      {active && <i />}
    </button>
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
  const create = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    try {
      const data = await read<{ createHousehold: { id: string } }>(createHouseholdMutation, {
        input: { name, memberNames: [] },
      });
      onCreated(data.createHousehold.id);
    } catch (reason) {
      window.alert((reason as Error).message);
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
            <button key={item.id} className="household-option" onClick={() => onSelect(item.id)}>
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
        <button className="button button-primary" disabled={pending}>
          家計簿を作成 <Plus size={16} />
        </button>
      </form>
    </section>
  );
}

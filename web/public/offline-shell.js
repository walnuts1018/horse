const scopeStorageKey = "horse:offline-draft-scope:v1";
const databaseName = "horse-browser-data";
const databaseVersion = 4;
const draftsStore = "purchase-drafts-v3";

function readScope() {
  try {
    const value = JSON.parse(localStorage.getItem(scopeStorageKey) ?? "null");
    if (
      value?.version !== 1 ||
      typeof value.memberId !== "string" ||
      !value.memberId.trim() ||
      value.memberId !== value.memberId.trim() ||
      value.memberId.length > 200 ||
      typeof value.householdId !== "string" ||
      !value.householdId.trim() ||
      value.householdId !== value.householdId.trim() ||
      value.householdId.length > 200
    )
      return undefined;
    return { memberId: value.memberId, householdId: value.householdId };
  } catch {
    return undefined;
  }
}

function openExistingDatabase() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(databaseName);
    request.onupgradeneeded = () => request.transaction?.abort();
    request.onerror = () => resolve(undefined);
    request.onsuccess = () => {
      const database = request.result;
      if (
        database.version !== databaseVersion ||
        !database.objectStoreNames.contains(draftsStore)
      ) {
        database.close();
        resolve(undefined);
        return;
      }
      resolve(database);
    };
    request.onblocked = () => {
      request.transaction?.abort();
      reject(new Error("データベースを開けません"));
    };
  });
}

function isPurchaseDraft(value, storageKey) {
  return (
    value &&
    value.storageKey === storageKey &&
    value.cleared !== true &&
    typeof value.merchant === "string" &&
    typeof value.occurredAt === "string" &&
    typeof value.itemName === "string" &&
    typeof value.itemAmount === "string" &&
    typeof value.categoryId === "string" &&
    Array.isArray(value.items)
  );
}

function addInput(parent, labelText, name, value, type = "text", inputMode) {
  const label = document.createElement("label");
  label.append(document.createTextNode(labelText));
  const input = document.createElement("input");
  input.name = name;
  input.value = typeof value === "string" ? value : "";
  input.type = type;
  if (inputMode) input.inputMode = inputMode;
  label.append(input);
  parent.append(label);
}

function showMessage(title, message) {
  document.querySelector("#offline-title").textContent = title;
  document.querySelector("#offline-message").textContent = message;
}

async function start() {
  const scope = readScope();
  const requestedHouseholdId = /^\/households\/([^/]+)\/new\/?$/.exec(location.pathname)?.[1];
  const offlineEditor = document.querySelector("#offline-editor");
  const openDraft = document.querySelector("#open-draft");
  const returnOnline = document.querySelector("#return-online");
  const form = document.querySelector("#draft-form");
  document.querySelector("#offline-logout").addEventListener("click", () => {
    try {
      localStorage.removeItem(scopeStorageKey);
    } catch {
      showMessage("下書きを隠せませんでした", "");
      return;
    }
    offlineEditor.hidden = true;
    openDraft.hidden = true;
    form.reset();
    document.querySelector("#draft-items").replaceChildren();
    showMessage("下書きを隠しました", "端末内の下書きは削除されていません。");
  });
  if (!scope) {
    showMessage("下書きを開けません", "オンラインでログインしてください。");
    return;
  }
  offlineEditor.hidden = false;

  let householdId = scope.householdId;
  try {
    if (requestedHouseholdId) householdId = decodeURIComponent(requestedHouseholdId);
  } catch {
    showMessage("下書きを開けません", "オンラインで再読み込みしてください。");
    return;
  }
  const target = `/households/${encodeURIComponent(householdId)}/new`;
  openDraft.href = target;
  openDraft.hidden = Boolean(requestedHouseholdId);
  returnOnline.href = target;
  if (householdId !== scope.householdId) {
    showMessage("下書きを開けません", "オンラインでこの家計簿を確認してください。");
    return;
  }

  showMessage("購入下書き", "");
  offlineEditor.hidden = false;
  try {
    if (typeof indexedDB === "undefined") throw new Error("IndexedDB is unavailable");
    const database = await openExistingDatabase();
    if (!database) {
      showMessage("下書きがありません", "");
      return;
    }
    const storageKey = JSON.stringify([scope.memberId, scope.householdId]);
    let draft;
    try {
      const transaction = database.transaction(draftsStore, "readonly");
      draft = await new Promise((resolve, reject) => {
        const request = transaction.objectStore(draftsStore).get(storageKey);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
      await new Promise((resolve, reject) => {
        transaction.oncomplete = resolve;
        transaction.onerror = () => reject(transaction.error);
        transaction.onabort = () => reject(transaction.error);
      });
    } finally {
      database.close();
    }
    if (!isPurchaseDraft(draft, storageKey)) {
      showMessage("下書きがありません", "");
      return;
    }

    let currentDraft = structuredClone(draft);
    let hasUnsavedChanges = false;
    const itemList = document.querySelector("#draft-items");
    const status = document.querySelector("#offline-status");
    const readEditedItems = () =>
      currentDraft.items.map((item, index) => ({
        ...item,
        name: form.elements.namedItem(`item-${index}-name`)?.value ?? item.name,
        amount: form.elements.namedItem(`item-${index}-amount`)?.value ?? item.amount,
      }));
    const renderItems = () => {
      itemList.replaceChildren();
      currentDraft.items.forEach((item, index) => {
        const fieldset = document.createElement("fieldset");
        const legend = document.createElement("legend");
        legend.textContent = `品目 ${index + 1}`;
        fieldset.append(legend);
        addInput(fieldset, "商品名", `item-${index}-name`, item.name);
        addInput(fieldset, "金額", `item-${index}-amount`, item.amount, "text", "numeric");
        const remove = document.createElement("button");
        remove.className = "secondary";
        remove.type = "button";
        remove.dataset.removeItem = String(index);
        remove.textContent = "品目を削除";
        fieldset.append(remove);
        itemList.append(fieldset);
      });
    };

    form.elements.namedItem("merchant").value = currentDraft.merchant;
    form.elements.namedItem("occurredAt").value = currentDraft.occurredAt;
    form.elements.namedItem("itemName").value = currentDraft.itemName;
    form.elements.namedItem("itemAmount").value = currentDraft.itemAmount;
    renderItems();
    form.hidden = false;
    openDraft.hidden = true;
    document.querySelector("#offline-message").textContent = "";

    document.querySelector("#add-item").addEventListener("click", () => {
      hasUnsavedChanges = true;
      currentDraft.items = readEditedItems();
      currentDraft.items.push({
        key: crypto.randomUUID(),
        name: "",
        amount: "",
        categoryId: "",
        allocations: [],
      });
      renderItems();
    });
    itemList.addEventListener("click", (event) => {
      const button = event.target.closest("[data-remove-item]");
      if (!button) return;
      hasUnsavedChanges = true;
      currentDraft.items = readEditedItems();
      currentDraft.items.splice(Number(button.dataset.removeItem), 1);
      renderItems();
    });
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const nextDraft = {
        ...currentDraft,
        merchant: form.elements.namedItem("merchant").value,
        occurredAt: form.elements.namedItem("occurredAt").value,
        itemName: form.elements.namedItem("itemName").value,
        itemAmount: form.elements.namedItem("itemAmount").value,
        items: readEditedItems(),
      };
      try {
        const writeDatabase = await openExistingDatabase();
        if (!writeDatabase) throw new Error("The local draft database is unavailable");
        try {
          const writeTransaction = writeDatabase.transaction(draftsStore, "readwrite");
          writeTransaction.objectStore(draftsStore).put(nextDraft);
          await new Promise((resolve, reject) => {
            writeTransaction.oncomplete = resolve;
            writeTransaction.onerror = () => reject(writeTransaction.error);
            writeTransaction.onabort = () => reject(writeTransaction.error);
          });
        } finally {
          writeDatabase.close();
        }
        currentDraft = nextDraft;
        hasUnsavedChanges = false;
        status.textContent = "保存しました";
      } catch {
        status.textContent = "保存できませんでした";
      }
    });
    form.addEventListener("input", () => {
      hasUnsavedChanges = true;
    });
    form.addEventListener("change", () => {
      hasUnsavedChanges = true;
    });
    window.addEventListener("beforeunload", (event) => {
      if (!hasUnsavedChanges) return;
      event.preventDefault();
      event.returnValue = "";
    });
  } catch {
    showMessage("下書きを開けません", "オンラインで再読み込みしてください。");
  }
}

void start();

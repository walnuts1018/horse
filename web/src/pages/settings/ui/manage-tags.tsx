import styles from "./settings-page.module.css";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Plus } from "lucide-react";
import { type Tag } from "../../../shared/api";
import { InlineError } from "../../../shared/ui";
import { readTags } from "../../../entities/tag";
import { archiveTag, createTag, updateTag } from "../api/tag-management";

export function ManageTags({
  householdId,
  hidden,
  onNotice,
  onFormDirty,
  onFormClean,
  onPendingChange,
}: {
  householdId: string;
  hidden: boolean;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormDirty: (formID: string) => void;
  onFormClean: (formID: string) => void;
  onPendingChange: (pending: boolean) => void;
}) {
  const [tags, setTags] = useState<Tag[]>([]);
  const [tagName, setTagName] = useState("");
  const [tagDrafts, setTagDrafts] = useState<Record<string, string>>({});
  const [pending, setPending] = useState(false);
  const [loadError, setLoadError] = useState("");

  const refresh = useCallback(
    async (savedTagId?: string, resetDrafts = false) => {
      setLoadError("");
      try {
        const result = await readTags(householdId, true);
        setTags(result);
        setTagDrafts((current) =>
          Object.fromEntries(
            result.map((tag) => [
              tag.id,
              resetDrafts || tag.id === savedTagId ? tag.name : (current[tag.id] ?? tag.name),
            ]),
          ),
        );
      } catch (reason) {
        setLoadError((reason as Error).message);
        throw reason;
      }
    },
    [householdId],
  );

  useEffect(() => {
    void refresh(undefined, true).catch(() => undefined);
  }, [refresh]);

  const saveTag = async (event: FormEvent) => {
    event.preventDefault();
    const name = tagName.trim();
    if (!name) return;
    setPending(true);
    onPendingChange(true);
    try {
      await createTag({ householdId, name });
      onFormClean("create-tag");
      setTagName("");
      await refresh();
      onNotice("タグを追加しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  const renameTag = async (tag: Tag) => {
    const name = (tagDrafts[tag.id] ?? tag.name).trim();
    if (!name || name === tag.name) return;
    setPending(true);
    onPendingChange(true);
    try {
      await updateTag({ tagId: tag.id, baseRevision: tag.revision, name });
      onFormClean(`tag:${tag.id}`);
      await refresh(tag.id);
      onNotice("タグ名を保存しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  const toggleTagVisibility = async (tag: Tag) => {
    setPending(true);
    onPendingChange(true);
    try {
      await archiveTag({ tagId: tag.id, baseRevision: tag.revision, archived: !tag.archived });
      await refresh();
      onNotice(tag.archived ? "タグを再表示しました" : "タグを非表示にしました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  return (
    <section className={`panel ${styles.settingsSection}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">品目管理</span>
          <h2>タグ</h2>
        </div>
        <span className="count-badge">{tags.filter((tag) => !tag.archived).length}</span>
      </div>
      {loadError && <InlineError message={loadError} />}
      <div className={styles.settingsRows}>
        {tags.map((tag) => (
          <div className={styles.settingRow} key={tag.id}>
            <input
              aria-label={`${tag.name}のタグ名`}
              value={tagDrafts[tag.id] ?? tag.name}
              disabled={tag.archived}
              onChange={(event) => {
                setTagDrafts((current) => ({ ...current, [tag.id]: event.target.value }));
                if (event.target.value === tag.name) onFormClean(`tag:${tag.id}`);
                else onFormDirty(`tag:${tag.id}`);
              }}
            />
            <button
              className="text-button"
              disabled={
                pending || tag.archived || (tagDrafts[tag.id] ?? tag.name).trim() === tag.name
              }
              onClick={() => void renameTag(tag)}
            >
              保存
            </button>
            {(tagDrafts[tag.id] ?? tag.name) !== tag.name && (
              <button
                type="button"
                className="text-button"
                onClick={() => {
                  setTagDrafts((current) => ({ ...current, [tag.id]: tag.name }));
                  onFormClean(`tag:${tag.id}`);
                }}
              >
                取消
              </button>
            )}
            <button
              className="text-button"
              disabled={pending}
              onClick={() => void toggleTagVisibility(tag)}
            >
              {tag.archived ? "再表示" : "非表示"}
            </button>
          </div>
        ))}
      </div>
      <form
        className="inline-create"
        data-unsaved-id="create-tag"
        onSubmit={(event) => void saveTag(event)}
      >
        <input
          value={tagName}
          onChange={(event) => setTagName(event.target.value)}
          placeholder="タグ名"
          maxLength={80}
          required
        />
        <button className="button button-soft" disabled={pending}>
          <Plus size={15} />
          追加
        </button>
      </form>
    </section>
  );
}

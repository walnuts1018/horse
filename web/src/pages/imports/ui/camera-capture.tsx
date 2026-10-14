import { X } from "lucide-react";
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
  type PointerEvent,
} from "react";

import { errorMessage } from "../../../shared/lib/errors";
import {
  cropCameraImage,
  identityCameraImageTransform,
  rotateCameraImageClockwise,
  type CameraImageTransform,
} from "../model/camera-image-transform";
import { isSupportedFile } from "../model/imports";

import "./camera-capture.css";

type Crop = { left: number; top: number; right: number; bottom: number };
type CameraDraft = {
  originalFile: File;
  file: File;
  src: string;
  crop: Crop | null;
  transform: CameraImageTransform;
};
type Point = { x: number; y: number };

const supportedImageTypes = new Set(["image/jpeg", "image/png", "image/webp"]);
const supportedImageExtensions = /\.(jpe?g|png|webp)$/i;

const isSupportedCameraImage = (file: File) =>
  isSupportedFile(file) &&
  (supportedImageTypes.has(file.type) || supportedImageExtensions.test(file.name));

export type CameraCaptureHandle = { open: () => void };

export const CameraCapture = forwardRef<
  CameraCaptureHandle,
  {
    onAdd: (upload: {
      originalFile: File;
      analysisFile?: File;
      analysisTransform?: CameraImageTransform;
    }) => void;
    onError: (message: string) => void;
  }
>(function CameraCapture({ onAdd, onError }, ref) {
  const inputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const pointerStart = useRef<{ point: Point; client: Point; pan: Point } | null>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const [draft, setDraft] = useState<CameraDraft | null>(null);
  const [cropMode, setCropMode] = useState(false);
  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState<Point>({ x: 0, y: 0 });
  const draftOpen = draft !== null;

  useImperativeHandle(ref, () => ({
    open: () => {
      returnFocus.current =
        document.activeElement instanceof HTMLElement ? document.activeElement : null;
      inputRef.current?.click();
    },
  }));

  useEffect(() => {
    const src = draft?.src;
    if (!src) return;
    return () => URL.revokeObjectURL(src);
  }, [draft?.src]);

  useEffect(() => {
    if (!draftOpen) return;
    closeRef.current?.focus();
    const handleDialogKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setDraft(null);
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = dialogRef.current?.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
      );
      if (!focusable?.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (
        event.shiftKey &&
        (document.activeElement === first || !dialogRef.current?.contains(document.activeElement))
      ) {
        event.preventDefault();
        last.focus();
      } else if (
        !event.shiftKey &&
        (document.activeElement === last || !dialogRef.current?.contains(document.activeElement))
      ) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", handleDialogKeyDown);
    return () => {
      document.removeEventListener("keydown", handleDialogKeyDown);
      returnFocus.current?.focus();
    };
  }, [draftOpen]);

  const reviewFile = (files: FileList | null) => {
    const file = files?.[0];
    if (!file) return;
    if (!isSupportedCameraImage(file)) {
      onError("画像を選択してください");
      return;
    }
    setCropMode(false);
    setZoom(1);
    setPan({ x: 0, y: 0 });
    setDraft({
      originalFile: file,
      file,
      src: URL.createObjectURL(file),
      crop: null,
      transform: identityCameraImageTransform,
    });
  };

  const rotate = async () => {
    if (!draft) return;
    try {
      const image = await loadImage(draft.src);
      const canvas = document.createElement("canvas");
      canvas.width = image.naturalHeight;
      canvas.height = image.naturalWidth;
      const context = canvas.getContext("2d");
      if (!context) throw new Error("画像を回転できませんでした");
      context.translate(canvas.width, 0);
      context.rotate(Math.PI / 2);
      context.drawImage(image, 0, 0);
      const type = ["image/jpeg", "image/png", "image/webp"].includes(draft.file.type)
        ? draft.file.type
        : "image/jpeg";
      const blob = await canvasBlob(canvas, type);
      const file = new File([blob], draft.file.name, {
        type,
        lastModified: draft.file.lastModified,
      });
      const transform = rotateCameraImageClockwise(draft.transform);
      setDraft({ ...draft, file, src: URL.createObjectURL(file), crop: null, transform });
      setZoom(1);
      setPan({ x: 0, y: 0 });
    } catch (reason) {
      onError(errorMessage(reason));
    }
  };

  const updateCrop = (event: PointerEvent<HTMLDivElement>, finish = false) => {
    const bounds = event.currentTarget.getBoundingClientRect();
    const start = pointerStart.current;
    if (!start) return;
    const point = {
      x: Math.max(0, Math.min(1, (event.clientX - bounds.left - pan.x) / (bounds.width * zoom))),
      y: Math.max(0, Math.min(1, (event.clientY - bounds.top - pan.y) / (bounds.height * zoom))),
    };
    if (!cropMode) {
      if (zoom > 1) {
        setPan({
          x: start.pan.x + event.clientX - start.client.x,
          y: start.pan.y + event.clientY - start.client.y,
        });
      }
      if (finish) pointerStart.current = null;
      return;
    }
    const crop = {
      left: Math.min(start.point.x, point.x),
      top: Math.min(start.point.y, point.y),
      right: Math.max(start.point.x, point.x),
      bottom: Math.max(start.point.y, point.y),
    };
    if (finish) {
      pointerStart.current = null;
      if (crop.right - crop.left < 0.02 || crop.bottom - crop.top < 0.02) {
        setDraft((current) => (current ? { ...current, crop: null } : current));
        return;
      }
    }
    setDraft((current) => (current ? { ...current, crop } : current));
  };

  const add = async () => {
    if (!draft) return;
    try {
      let analysisFile: File | undefined;
      let analysisTransform: CameraImageTransform | undefined;
      if (draft.crop) {
        const image = await loadImage(draft.src);
        const { left, top, right, bottom } = draft.crop;
        const x = Math.round(left * image.naturalWidth);
        const y = Math.round(top * image.naturalHeight);
        const width = Math.max(1, Math.round((right - left) * image.naturalWidth));
        const height = Math.max(1, Math.round((bottom - top) * image.naturalHeight));
        const canvas = document.createElement("canvas");
        canvas.width = width;
        canvas.height = height;
        const context = canvas.getContext("2d");
        if (!context) throw new Error("画像を切り抜けませんでした");
        context.drawImage(image, x, y, width, height, 0, 0, width, height);
        const blob = await canvasBlob(canvas, draft.file.type || "image/jpeg");
        analysisFile = new File([blob], draft.file.name, {
          type: blob.type,
          lastModified: draft.file.lastModified,
        });
        analysisTransform = cropCameraImage(draft.transform, draft.crop);
      } else if (
        draft.transform.some((value, index) => value !== identityCameraImageTransform[index])
      ) {
        analysisFile = draft.file;
        analysisTransform = draft.transform;
      }
      onAdd({ originalFile: draft.originalFile, analysisFile, analysisTransform });
      setDraft(null);
    } catch (reason) {
      onError(errorMessage(reason));
    }
  };

  return (
    <>
      <input
        ref={inputRef}
        type="file"
        accept="image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp"
        capture="environment"
        hidden
        onChange={(event) => {
          reviewFile(event.currentTarget.files);
          event.currentTarget.value = "";
        }}
      />
      {draft && (
        <div
          role="presentation"
          onClick={(event) => {
            if (event.target === event.currentTarget) setDraft(null);
          }}
          style={{
            position: "fixed",
            inset: 0,
            zIndex: 1000,
            display: "grid",
            placeItems: "center",
            padding: 16,
            background: "rgb(20 30 24 / 68%)",
          }}
        >
          <dialog
            ref={dialogRef}
            aria-modal="true"
            aria-labelledby="camera-review-title"
            className="panel camera-capture-dialog"
            style={{ width: "min(720px, 100%)", maxHeight: "100%", overflow: "auto", padding: 20 }}
            open
          >
            <div className="panel-heading">
              <div>
                <span className="eyebrow">撮影画像</span>
                <h2 id="camera-review-title">内容を確認</h2>
              </div>
              <button
                ref={closeRef}
                type="button"
                className="icon-button"
                aria-label="撮影画像を破棄"
                onClick={() => setDraft(null)}
              >
                <X size={16} />
              </button>
            </div>
            <div
              className={`camera-capture-stage${cropMode ? " is-cropping" : ""}`}
              onPointerDown={(event) => {
                const bounds = event.currentTarget.getBoundingClientRect();
                const point = {
                  x: Math.max(
                    0,
                    Math.min(1, (event.clientX - bounds.left - pan.x) / (bounds.width * zoom)),
                  ),
                  y: Math.max(
                    0,
                    Math.min(1, (event.clientY - bounds.top - pan.y) / (bounds.height * zoom)),
                  ),
                };
                pointerStart.current = {
                  point,
                  client: { x: event.clientX, y: event.clientY },
                  pan,
                };
                event.currentTarget.setPointerCapture(event.pointerId);
                if (cropMode) {
                  setDraft((current) =>
                    current
                      ? {
                          ...current,
                          crop: { left: point.x, top: point.y, right: point.x, bottom: point.y },
                        }
                      : current,
                  );
                }
              }}
              onPointerMove={(event) => updateCrop(event)}
              onPointerUp={(event) => updateCrop(event, true)}
              onPointerCancel={() => {
                pointerStart.current = null;
              }}
            >
              <div
                className="camera-capture-image"
                style={{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})` }}
              >
                <img src={draft.src} alt="撮影した画像" />
                {draft.crop && (
                  <div
                    className="camera-capture-selection"
                    aria-hidden="true"
                    style={{
                      left: `${draft.crop.left * 100}%`,
                      top: `${draft.crop.top * 100}%`,
                      width: `${(draft.crop.right - draft.crop.left) * 100}%`,
                      height: `${(draft.crop.bottom - draft.crop.top) * 100}%`,
                    }}
                  />
                )}
              </div>
            </div>
            <div className="import-actions" style={{ justifyContent: "flex-end" }}>
              <button
                type="button"
                className="button button-soft"
                aria-pressed={cropMode}
                onClick={() => setCropMode((current) => !current)}
              >
                {cropMode ? "切り抜きモードを終了" : "切り抜き"}
              </button>
              <button
                type="button"
                className="button button-soft"
                aria-label="縮小"
                disabled={zoom <= 1}
                onClick={() => setZoom((current) => Math.max(1, current - 0.25))}
              >
                −
              </button>
              <button
                type="button"
                className="button button-soft"
                aria-label="拡大"
                disabled={zoom >= 4}
                onClick={() => setZoom((current) => Math.min(4, current + 0.25))}
              >
                ＋
              </button>
              <button
                type="button"
                className="button button-soft"
                disabled={zoom === 1 && pan.x === 0 && pan.y === 0}
                onClick={() => {
                  setZoom(1);
                  setPan({ x: 0, y: 0 });
                }}
              >
                表示をリセット
              </button>
              <button type="button" className="button button-soft" onClick={() => void rotate()}>
                回転
              </button>
              {draft.crop && (
                <button
                  type="button"
                  className="button button-soft"
                  onClick={() =>
                    setDraft((current) => (current ? { ...current, crop: null } : current))
                  }
                >
                  切り抜きを解除
                </button>
              )}
              <button type="button" className="button button-primary" onClick={() => void add()}>
                画像をアップロード
              </button>
            </div>
            {draft.crop && (
              <fieldset className="allocation-rule-form">
                <legend>切り抜き範囲</legend>
                {(
                  [
                    ["左端", "left", 0, draft.crop.right * 100 - 2, draft.crop.left],
                    ["上端", "top", 0, draft.crop.bottom * 100 - 2, draft.crop.top],
                    ["右端", "right", draft.crop.left * 100 + 2, 100, draft.crop.right],
                    ["下端", "bottom", draft.crop.top * 100 + 2, 100, draft.crop.bottom],
                  ] as const
                ).map(([label, edge, min, max, value]) => (
                  <label key={edge}>
                    {label}
                    <input
                      type="range"
                      min={min}
                      max={max}
                      step="1"
                      value={value * 100}
                      onChange={(event) => {
                        const nextValue = Number(event.target.value) / 100;
                        setDraft((current) =>
                          current?.crop
                            ? { ...current, crop: { ...current.crop, [edge]: nextValue } }
                            : current,
                        );
                      }}
                    />
                  </label>
                ))}
              </fieldset>
            )}
          </dialog>
        </div>
      )}
    </>
  );
});

const loadImage = (src: string) =>
  new Promise<HTMLImageElement>((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("画像を読み込めませんでした"));
    image.src = src;
  });

const canvasBlob = (canvas: HTMLCanvasElement, type: string) =>
  new Promise<Blob>((resolve, reject) => {
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error("画像を変換できませんでした"))),
      type,
      0.92,
    );
  });

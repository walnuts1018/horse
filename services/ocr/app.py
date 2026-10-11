import asyncio
import io
import os
from contextlib import asynccontextmanager

import numpy as np
from fastapi import FastAPI, File, HTTPException, UploadFile
from PIL import Image, UnidentifiedImageError
from starlette.concurrency import run_in_threadpool

MAX_BYTES = 20 << 20
Image.MAX_IMAGE_PIXELS = 40_000_000
slot = asyncio.Lock()


@asynccontextmanager
async def lifespan(app: FastAPI):
    from paddleocr import PaddleOCR

    app.state.pipeline = await run_in_threadpool(
        PaddleOCR,
        text_detection_model_name=os.getenv("OCR_DETECTION_MODEL", "PP-OCRv6_medium_det"),
        text_recognition_model_name=os.getenv("OCR_RECOGNITION_MODEL", "PP-OCRv6_medium_rec"),
        use_doc_orientation_classify=False,
        use_doc_unwarping=False,
        use_textline_orientation=True,
        device="cpu",
        cpu_threads=int(os.getenv("OCR_CPU_THREADS", "2")),
    )
    yield


app = FastAPI(lifespan=lifespan)


@app.get("/healthz")
@app.get("/readyz")
def health():
    if not hasattr(app.state, "pipeline"):
        raise HTTPException(status_code=503, detail="Model is loading")
    return {"ready": True}


def extract_lines(data: bytes):
    try:
        with Image.open(io.BytesIO(data)) as image:
            if image.width * image.height > Image.MAX_IMAGE_PIXELS:
                raise ValueError("Image is too large")
            width, height = image.size
            pixels = np.array(image.convert("RGB"))
    except (UnidentifiedImageError, OSError, Image.DecompressionBombError, ValueError) as error:
        raise HTTPException(status_code=400, detail="Invalid image") from error
    lines = []
    for result in app.state.pipeline.predict(pixels):
        payload = result.json
        if callable(payload):
            payload = payload()
        if isinstance(payload, str):
            import json

            payload = json.loads(payload)
        payload = payload.get("res", payload)
        texts = payload.get("rec_texts", [])
        scores = payload.get("rec_scores", [])
        boxes = payload.get("rec_boxes", [])
        for text, score, box in zip(texts, scores, boxes, strict=True):
            left, top, right, bottom = map(float, box)
            if left >= right or top >= bottom:
                continue
            lines.append(
                {
                    "text": text,
                    "confidence": float(score),
                    "region": {
                        "page": 0,
                        "top": max(0, min(1, top / height)),
                        "left": max(0, min(1, left / width)),
                        "bottom": max(0, min(1, bottom / height)),
                        "right": max(0, min(1, right / width)),
                    },
                }
            )
    return {"lines": lines}


@app.post("/extract")
async def extract(image: UploadFile = File(...)):
    try:
        data = await image.read(MAX_BYTES + 1)
        if not data or len(data) > MAX_BYTES:
            raise HTTPException(status_code=413, detail="Image exceeds size limit")
        async with slot:
            return await run_in_threadpool(extract_lines, data)
    finally:
        await image.close()

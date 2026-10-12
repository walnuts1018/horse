import asyncio
import io
import json
import math
import os
from contextlib import asynccontextmanager

import numpy as np
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from PIL import Image, ImageOps, UnidentifiedImageError
from starlette.concurrency import run_in_threadpool
from starlette.responses import Response

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


def crop_document(data: bytes, page: int, top: float, left: float, bottom: float, right: float):
    if not 0 <= top < bottom <= 1 or not 0 <= left < right <= 1:
        raise HTTPException(status_code=400, detail="Invalid crop region")
    try:
        if data.startswith(b"%PDF-"):
            import pypdfium2 as pdfium

            pdf = pdfium.PdfDocument(data)
            if page < 0 or page >= len(pdf):
                raise HTTPException(status_code=400, detail="PDF page is out of range")
            pdf_page = pdf[page]
            page_width, page_height = pdf_page.get_size()
            if page_width <= 0 or page_height <= 0:
                raise HTTPException(status_code=400, detail="Invalid PDF page")
            scale = min(2, math.sqrt(Image.MAX_IMAGE_PIXELS / (page_width * page_height)))
            source = pdf_page.render(scale=scale).to_pil().convert("RGB")
        else:
            if page != 0:
                raise HTTPException(status_code=400, detail="Image page is out of range")
            source = Image.open(io.BytesIO(data))
            source.load()
            source = ImageOps.exif_transpose(source).convert("RGB")
        if source.width * source.height > Image.MAX_IMAGE_PIXELS:
            raise HTTPException(status_code=400, detail="Image is too large")
    except HTTPException:
        raise
    except (UnidentifiedImageError, OSError, Image.DecompressionBombError, ValueError) as error:
        raise HTTPException(status_code=400, detail="Invalid image or PDF") from error
    width, height = source.size
    x0 = max(0, min(width - 1, math.floor(left * width)))
    y0 = max(0, min(height - 1, math.floor(top * height)))
    x1 = max(x0 + 1, min(width, math.ceil(right * width)))
    y1 = max(y0 + 1, min(height, math.ceil(bottom * height)))
    output = io.BytesIO()
    source.crop((x0, y0, x1, y1)).save(output, format="PNG")
    transform = [
        (x1 - x0) / width,
        0,
        x0 / width,
        0,
        (y1 - y0) / height,
        y0 / height,
        0,
        0,
        1,
    ]
    return output.getvalue(), transform


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


@app.post("/crop")
async def crop(
    image: UploadFile = File(...),
    page: int = Form(...),
    top: float = Form(...),
    left: float = Form(...),
    bottom: float = Form(...),
    right: float = Form(...),
):
    try:
        data = await image.read(MAX_BYTES + 1)
        if not data or len(data) > MAX_BYTES:
            raise HTTPException(status_code=413, detail="Document exceeds size limit")
        async with slot:
            output, transform = await run_in_threadpool(crop_document, data, page, top, left, bottom, right)
        if len(output) > MAX_BYTES:
            raise HTTPException(status_code=413, detail="Cropped image exceeds size limit")
        return Response(
            content=output,
            media_type="image/png",
            headers={"X-Coordinate-Transform": json.dumps(transform, separators=(",", ":"))},
        )
    finally:
        await image.close()

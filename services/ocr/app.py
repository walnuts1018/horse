import asyncio
import io
import json
import math
import os
import time
from contextlib import asynccontextmanager

import numpy as np
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from PIL import Image, ImageOps, UnidentifiedImageError
from starlette.concurrency import run_in_threadpool
from starlette.responses import Response

MAX_BYTES = 20 << 20
Image.MAX_IMAGE_PIXELS = 40_000_000
MAX_PDF_PAGES = 20
MAX_PDF_PAGE_PIXELS = 4_000_000
MAX_PDF_TOTAL_PIXELS = 40_000_000
MAX_OCR_LINES = 10_000
MAX_OCR_SECONDS = 120
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


def extract_page_lines(pixels: np.ndarray, width: int, height: int, page: int, deadline: float):
    if time.monotonic() >= deadline:
        raise HTTPException(status_code=504, detail="OCR exceeded the processing time limit")
    lines = []
    for result in app.state.pipeline.predict(pixels):
        payload = result.json
        if callable(payload):
            payload = payload()
        if isinstance(payload, str):
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
                        "page": page,
                        "top": max(0, min(1, top / height)),
                        "left": max(0, min(1, left / width)),
                        "bottom": max(0, min(1, bottom / height)),
                        "right": max(0, min(1, right / width)),
                    },
                }
            )
            if len(lines) > MAX_OCR_LINES:
                raise HTTPException(status_code=413, detail="OCR returned too many text lines")
    if time.monotonic() >= deadline:
        raise HTTPException(status_code=504, detail="OCR exceeded the processing time limit")
    return lines


def extract_pdf_lines(data: bytes, deadline: float):
    import pypdfium2 as pdfium

    pages = []
    try:
        pdf = pdfium.PdfDocument(data)
    except Exception as error:
        raise HTTPException(status_code=400, detail="Invalid PDF") from error
    try:
        page_count = len(pdf)
        if page_count < 1 or page_count > MAX_PDF_PAGES:
            raise HTTPException(status_code=413, detail="PDF page count exceeds the OCR limit")
        total_pixels = 0
        for page_number in range(page_count):
            page = None
            try:
                page = pdf[page_number]
                width, height = page.get_size()
            except Exception as error:
                if page is not None:
                    page.close()
                raise HTTPException(status_code=400, detail="Invalid PDF page") from error
            if not math.isfinite(width) or not math.isfinite(height) or width <= 0 or height <= 0:
                page.close()
                raise HTTPException(status_code=400, detail="Invalid PDF page dimensions")
            scale = min(2, math.sqrt(MAX_PDF_PAGE_PIXELS / (width * height)))
            pages.append((page, scale))
            expected_width = max(1, math.ceil(width * scale))
            expected_height = max(1, math.ceil(height * scale))
            page_pixels = expected_width * expected_height
            total_pixels += page_pixels
            if page_pixels > MAX_PDF_PAGE_PIXELS or total_pixels > MAX_PDF_TOTAL_PIXELS:
                raise HTTPException(status_code=413, detail="PDF raster size exceeds the OCR limit")

        lines = []
        for page_number, (page, scale) in enumerate(pages):
            if time.monotonic() >= deadline:
                raise HTTPException(status_code=504, detail="OCR exceeded the processing time limit")
            bitmap = page.render(scale=scale)
            try:
                with bitmap.to_pil() as image:
                    with image.convert("RGB") as rgb_image:
                        width, height = rgb_image.size
                        if width * height > MAX_PDF_PAGE_PIXELS:
                            raise HTTPException(status_code=413, detail="PDF raster size exceeds the OCR limit")
                        pixels = np.array(rgb_image)
            finally:
                bitmap.close()
            lines.extend(extract_page_lines(pixels, width, height, page_number, deadline))
            if len(lines) > MAX_OCR_LINES:
                raise HTTPException(status_code=413, detail="OCR returned too many text lines")
        return {"pageCount": page_count, "lines": lines}
    finally:
        for page, _ in pages:
            page.close()
        pdf.close()


def extract_lines(data: bytes):
    deadline = time.monotonic() + MAX_OCR_SECONDS
    if data.startswith(b"%PDF-"):
        return extract_pdf_lines(data, deadline)
    try:
        with Image.open(io.BytesIO(data)) as image:
            oriented = ImageOps.exif_transpose(image)
            try:
                if oriented.width * oriented.height > Image.MAX_IMAGE_PIXELS:
                    raise ValueError("Image is too large")
                width, height = oriented.size
                with oriented.convert("RGB") as rgb_image:
                    pixels = np.array(rgb_image)
            finally:
                if oriented is not image:
                    oriented.close()
    except (UnidentifiedImageError, OSError, Image.DecompressionBombError, ValueError) as error:
        raise HTTPException(status_code=400, detail="Invalid image") from error
    return {"pageCount": 1, "lines": extract_page_lines(pixels, width, height, 0, deadline)}


def crop_document(data: bytes, page: int, top: float, left: float, bottom: float, right: float):
    if not 0 <= top < bottom <= 1 or not 0 <= left < right <= 1:
        raise HTTPException(status_code=400, detail="Invalid crop region")
    pdf = None
    pdf_page = None
    bitmap = None
    source = None
    try:
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
                bitmap = pdf_page.render(scale=scale)
                with bitmap.to_pil() as rendered:
                    source = rendered.convert("RGB")
            else:
                if page != 0:
                    raise HTTPException(status_code=400, detail="Image page is out of range")
                with Image.open(io.BytesIO(data)) as image:
                    image.load()
                    oriented = ImageOps.exif_transpose(image)
                    try:
                        source = oriented.convert("RGB")
                    finally:
                        if oriented is not image:
                            oriented.close()
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
        with source.crop((x0, y0, x1, y1)) as cropped:
            cropped.save(output, format="PNG")
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
    finally:
        if source is not None:
            source.close()
        if bitmap is not None:
            bitmap.close()
        if pdf_page is not None:
            pdf_page.close()
        if pdf is not None:
            pdf.close()


@app.post("/extract")
async def extract(image: UploadFile = File(...)):
    try:
        data = await image.read(MAX_BYTES + 1)
        if not data or len(data) > MAX_BYTES:
            raise HTTPException(status_code=413, detail="Document exceeds size limit")
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

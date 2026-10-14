import io
import sys
import unittest
from contextlib import contextmanager
from types import SimpleNamespace
from unittest.mock import patch

import numpy as np
from fastapi import HTTPException
from PIL import Image

from app import MAX_PDF_PAGES, crop_document, extract_lines


class CropDocumentTest(unittest.TestCase):
    def image_document(self, image_format):
        source = io.BytesIO()
        Image.new("RGB", (100, 80), "white").save(source, format=image_format)
        return source.getvalue()

    def test_webp_crop_returns_transform_without_mutating_original(self):
        original = self.image_document("WEBP")
        source = bytes(original)
        cropped, transform = crop_document(original, 0, 0.25, 0.2, 0.75, 0.8)

        with Image.open(io.BytesIO(cropped)) as image:
            self.assertEqual(image.size, (60, 40))
        self.assertEqual(original, source)
        self.assertEqual(transform, [0.6, 0, 0.2, 0, 0.5, 0.25, 0, 0, 1])

    def test_pdf_crop_renders_selected_page(self):
        original = self.image_document("PDF")
        cropped, transform = crop_document(original, 0, 0.25, 0.2, 0.75, 0.8)

        with Image.open(io.BytesIO(cropped)) as image:
            self.assertGreater(image.width, 0)
            self.assertGreater(image.height, 0)
        self.assertEqual(len(transform), 9)
        self.assertEqual(transform[-1], 1)

    def test_pdf_crop_closes_document_page_and_bitmap(self):
        class Bitmap:
            closed = False
            image_closed = False

            @contextmanager
            def to_pil(self):
                image = Image.new("RGB", (100, 80), "white")
                try:
                    yield image
                finally:
                    image.close()
                    self.image_closed = True

            def close(self):
                self.closed = True

        class PDFPage:
            closed = False

            def get_size(self):
                return 100, 80

            def render(self, scale):
                return bitmap

            def close(self):
                self.closed = True

        class PDF:
            closed = False

            def __len__(self):
                return 1

            def __getitem__(self, page):
                return pdf_page

            def close(self):
                self.closed = True

        bitmap = Bitmap()
        pdf_page = PDFPage()
        pdf = PDF()
        with patch.dict(sys.modules, {"pypdfium2": SimpleNamespace(PdfDocument=lambda data: pdf)}):
            crop_document(b"%PDF-test", 0, 0, 0, 1, 1)

        self.assertTrue(bitmap.closed)
        self.assertTrue(bitmap.image_closed)
        self.assertTrue(pdf_page.closed)
        self.assertTrue(pdf.closed)

    def test_jpeg_exif_rotation_uses_displayed_image_coordinates(self):
        source = io.BytesIO()
        exif = Image.Exif()
        exif[274] = 6
        Image.new("RGB", (100, 80), "white").save(source, format="JPEG", exif=exif)
        original = source.getvalue()

        cropped, transform = crop_document(original, 0, 0, 0, 1, 1)

        with Image.open(io.BytesIO(cropped)) as image:
            self.assertEqual(image.size, (80, 100))
        self.assertEqual(original, source.getvalue())
        self.assertEqual(transform, [1, 0, 0, 0, 1, 0, 0, 0, 1])

    def test_image_ocr_uses_exif_oriented_coordinates_for_all_orientations(self):
        expected = {
            1: ((80, 100), (10, 20, 30, 40)),
            2: ((80, 100), (70, 20, 90, 40)),
            3: ((80, 100), (70, 40, 90, 60)),
            4: ((80, 100), (10, 40, 30, 60)),
            5: ((100, 80), (20, 10, 40, 30)),
            6: ((100, 80), (40, 10, 60, 30)),
            7: ((100, 80), (40, 70, 60, 90)),
            8: ((100, 80), (20, 70, 40, 90)),
        }
        for orientation, (shape, (left, top, right, bottom)) in expected.items():
            with self.subTest(orientation=orientation):
                source = io.BytesIO()
                image = Image.new("RGB", (100, 80), "white")
                image.paste("black", (10, 20, 30, 40))
                exif = Image.Exif()
                exif[274] = orientation
                image.save(source, format="PNG", exif=exif)
                image.close()

                def predict(pixels):
                    ys, xs = np.where(np.all(pixels == 0, axis=2))
                    payload = {
                        "rec_texts": ["text"],
                        "rec_scores": [0.95],
                        "rec_boxes": [[xs.min(), ys.min(), xs.max() + 1, ys.max() + 1]],
                    }
                    return [SimpleNamespace(json={"res": payload})]

                with patch("app.app.state.pipeline", SimpleNamespace(predict=predict), create=True):
                    result = extract_lines(source.getvalue())

                width, height = shape[1], shape[0]
                self.assertEqual(result["lines"][0]["region"], {
                    "page": 0,
                    "top": top / height,
                    "left": left / width,
                    "bottom": bottom / height,
                    "right": right / width,
                })

    def test_crop_rejects_out_of_bounds_rectangle(self):
        with self.assertRaises(HTTPException):
            crop_document(self.image_document("PNG"), 0, 0.8, 0.2, 0.4, 0.8)


class PDFOCRTest(unittest.TestCase):
    def pdf_document(self, page_count):
        pages = [Image.new("RGB", (100, 80), "white") for _ in range(page_count)]
        source = io.BytesIO()
        pages[0].save(source, format="PDF", save_all=True, append_images=pages[1:])
        for page in pages:
            page.close()
        return source.getvalue()

    def test_extracts_ocr_lines_with_original_page_indices(self):
        payload = {
            "rec_texts": ["ヨーグルト 198"],
            "rec_scores": [0.95],
            "rec_boxes": [[10, 10, 90, 30]],
        }
        pipeline = SimpleNamespace(
            predict=lambda pixels: [SimpleNamespace(json={"res": payload})]
        )
        with patch("app.app.state.pipeline", pipeline, create=True):
            result = extract_lines(self.pdf_document(2))

        self.assertEqual(result["pageCount"], 2)
        self.assertEqual([line["region"]["page"] for line in result["lines"]], [0, 1])

    def test_rejects_pdf_over_page_limit_before_ocr(self):
        pipeline = SimpleNamespace(predict=lambda pixels: self.fail("OCR ran for oversized PDF"))
        with patch("app.app.state.pipeline", pipeline, create=True):
            with self.assertRaises(HTTPException) as raised:
                extract_lines(self.pdf_document(MAX_PDF_PAGES + 1))

        self.assertEqual(raised.exception.status_code, 413)


if __name__ == "__main__":
    unittest.main()

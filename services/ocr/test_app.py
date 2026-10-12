import io
import unittest

from fastapi import HTTPException
from PIL import Image

from app import crop_document


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

    def test_crop_rejects_out_of_bounds_rectangle(self):
        with self.assertRaises(HTTPException):
            crop_document(self.image_document("PNG"), 0, 0.8, 0.2, 0.4, 0.8)


if __name__ == "__main__":
    unittest.main()

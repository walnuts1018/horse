import { describe, expect, it } from "vitest";

import {
  cropCameraImage,
  exifCameraImageTransform,
  identityCameraImageTransform,
  readExifOrientation,
  rotateCameraImageClockwise,
} from "./camera-image-transform";

const point = (transform: number[], x: number, y: number) => [
  transform[0] * x + transform[1] * y + transform[2],
  transform[3] * x + transform[4] * y + transform[5],
];

describe("camera image transform", () => {
  it.each([
    [1, [0, 0], [1, 0], [1, 1], [0, 1]],
    [2, [1, 0], [0, 0], [0, 1], [1, 1]],
    [3, [1, 1], [0, 1], [0, 0], [1, 0]],
    [4, [0, 1], [1, 1], [1, 0], [0, 0]],
    [5, [0, 0], [0, 1], [1, 1], [1, 0]],
    [6, [0, 1], [0, 0], [1, 0], [1, 1]],
    [7, [1, 1], [1, 0], [0, 0], [0, 1]],
    [8, [1, 0], [1, 1], [0, 1], [0, 0]],
  ])("maps EXIF orientation %i display coordinates to the original", (orientation, ...corners) => {
    const transform = exifCameraImageTransform(orientation);
    const displayCorners = [
      [0, 0],
      [1, 0],
      [1, 1],
      [0, 1],
    ];

    displayCorners.forEach(([x, y], index) => {
      expect(point(transform, x, y)).toEqual(corners[index]);
    });
  });

  it("maps clockwise rotated image coordinates back to the original", () => {
    const transform = rotateCameraImageClockwise(identityCameraImageTransform);

    expect(point(transform, 0, 0)).toEqual([0, 1]);
    expect(point(transform, 1, 0)).toEqual([0, 0]);
    expect(point(transform, 1, 1)).toEqual([1, 0]);
    expect(point(transform, 0, 1)).toEqual([1, 1]);
  });

  it("maps cropped image coordinates through a prior rotation", () => {
    const rotated = rotateCameraImageClockwise(identityCameraImageTransform);
    const transform = cropCameraImage(rotated, { left: 0.2, top: 0.25, right: 0.6, bottom: 0.75 });

    expect(point(transform, 0, 0)[0]).toBeCloseTo(0.25);
    expect(point(transform, 0, 0)[1]).toBeCloseTo(0.8);
    expect(point(transform, 1, 1)[0]).toBeCloseTo(0.75);
    expect(point(transform, 1, 1)[1]).toBeCloseTo(0.4);
  });

  it.each([1, 2, 3, 4, 5, 6, 7, 8])(
    "composes EXIF orientation %i with an explicit rotation and crop",
    (orientation) => {
      const transform = cropCameraImage(
        rotateCameraImageClockwise(exifCameraImageTransform(orientation)),
        { left: 0.2, top: 0.25, right: 0.6, bottom: 0.75 },
      );

      point(transform, 0, 0).forEach((value, index) =>
        expect(value).toBeCloseTo(
          point(rotateCameraImageClockwise(exifCameraImageTransform(orientation)), 0.2, 0.25)[
            index
          ],
        ),
      );
      point(transform, 1, 1).forEach((value, index) =>
        expect(value).toBeCloseTo(
          point(rotateCameraImageClockwise(exifCameraImageTransform(orientation)), 0.6, 0.75)[
            index
          ],
        ),
      );
    },
  );

  it.each([
    [true, 1],
    [false, 6],
  ])("reads EXIF orientation from a JPEG with littleEndian=%s", (littleEndian, orientation) => {
    expect(readExifOrientation(jpegWithOrientation(orientation, littleEndian))).toBe(orientation);
  });
});

const jpegWithOrientation = (orientation: number, littleEndian: boolean) => {
  const data = new ArrayBuffer(40);
  const bytes = new DataView(data);
  bytes.setUint16(0, 0xffd8);
  bytes.setUint16(2, 0xffe1);
  bytes.setUint16(4, 34);
  bytes.setUint32(6, 0x45786966);
  bytes.setUint16(10, 0);
  bytes.setUint16(12, littleEndian ? 0x4949 : 0x4d4d);
  bytes.setUint16(14, 42, littleEndian);
  bytes.setUint32(16, 8, littleEndian);
  bytes.setUint16(20, 1, littleEndian);
  bytes.setUint16(22, 0x0112, littleEndian);
  bytes.setUint16(24, 3, littleEndian);
  bytes.setUint32(26, 1, littleEndian);
  bytes.setUint16(30, orientation, littleEndian);
  bytes.setUint16(38, 0xffd9);
  return data;
};

import { describe, expect, it } from "vitest";

import {
  cropCameraImage,
  identityCameraImageTransform,
  rotateCameraImageClockwise,
} from "./camera-image-transform";

const point = (transform: number[], x: number, y: number) => [
  transform[0] * x + transform[1] * y + transform[2],
  transform[3] * x + transform[4] * y + transform[5],
];

describe("camera image transform", () => {
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
});

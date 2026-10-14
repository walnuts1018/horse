export type CameraImageTransform = [
  number,
  number,
  number,
  number,
  number,
  number,
  number,
  number,
  number,
];

export const identityCameraImageTransform: CameraImageTransform = [1, 0, 0, 0, 1, 0, 0, 0, 1];

const multiply = (
  left: CameraImageTransform,
  right: CameraImageTransform,
): CameraImageTransform => [
  left[0] * right[0] + left[1] * right[3] + left[2] * right[6],
  left[0] * right[1] + left[1] * right[4] + left[2] * right[7],
  left[0] * right[2] + left[1] * right[5] + left[2] * right[8],
  left[3] * right[0] + left[4] * right[3] + left[5] * right[6],
  left[3] * right[1] + left[4] * right[4] + left[5] * right[7],
  left[3] * right[2] + left[4] * right[5] + left[5] * right[8],
  left[6] * right[0] + left[7] * right[3] + left[8] * right[6],
  left[6] * right[1] + left[7] * right[4] + left[8] * right[7],
  left[6] * right[2] + left[7] * right[5] + left[8] * right[8],
];

export const rotateCameraImageClockwise = (transform: CameraImageTransform) =>
  multiply(transform, [0, 1, 0, -1, 0, 1, 0, 0, 1]);

export const cropCameraImage = (
  transform: CameraImageTransform,
  crop: { left: number; top: number; right: number; bottom: number },
) =>
  multiply(transform, [
    crop.right - crop.left,
    0,
    crop.left,
    0,
    crop.bottom - crop.top,
    crop.top,
    0,
    0,
    1,
  ]);

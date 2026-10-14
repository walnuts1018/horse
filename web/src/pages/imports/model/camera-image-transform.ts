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

export const exifCameraImageTransform = (orientation: number): CameraImageTransform => {
  switch (orientation) {
    case 2:
      return [-1, 0, 1, 0, 1, 0, 0, 0, 1];
    case 3:
      return [-1, 0, 1, 0, -1, 1, 0, 0, 1];
    case 4:
      return [1, 0, 0, 0, -1, 1, 0, 0, 1];
    case 5:
      return [0, 1, 0, 1, 0, 0, 0, 0, 1];
    case 6:
      return [0, 1, 0, -1, 0, 1, 0, 0, 1];
    case 7:
      return [0, -1, 1, -1, 0, 1, 0, 0, 1];
    case 8:
      return [0, -1, 1, 1, 0, 0, 0, 0, 1];
    default:
      return identityCameraImageTransform;
  }
};

export const readExifOrientation = (data: ArrayBuffer): number => {
  const bytes = new DataView(data);
  if (bytes.byteLength < 4 || bytes.getUint16(0) !== 0xffd8) return 1;

  let offset = 2;
  while (offset + 4 <= bytes.byteLength) {
    if (bytes.getUint8(offset) !== 0xff) return 1;
    while (offset < bytes.byteLength && bytes.getUint8(offset) === 0xff) offset += 1;
    if (offset >= bytes.byteLength) return 1;
    const marker = bytes.getUint8(offset);
    offset += 1;
    if (marker === 0xd9 || marker === 0xda) return 1;
    if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd7)) continue;

    if (offset + 2 > bytes.byteLength) return 1;
    const length = bytes.getUint16(offset);
    const segmentStart = offset + 2;
    const segmentEnd = offset + length;
    if (length < 2 || segmentEnd > bytes.byteLength) return 1;
    if (
      marker === 0xe1 &&
      length >= 16 &&
      bytes.getUint32(segmentStart) === 0x45786966 &&
      bytes.getUint16(segmentStart + 4) === 0
    ) {
      const tiffStart = segmentStart + 6;
      const byteOrder = bytes.getUint16(tiffStart);
      const littleEndian = byteOrder === 0x4949;
      if (!littleEndian && byteOrder !== 0x4d4d) return 1;
      if (bytes.getUint16(tiffStart + 2, littleEndian) !== 42) return 1;
      const ifdOffset = bytes.getUint32(tiffStart + 4, littleEndian);
      const ifdStart = tiffStart + ifdOffset;
      if (ifdStart + 2 > segmentEnd) return 1;
      const entries = bytes.getUint16(ifdStart, littleEndian);
      for (let index = 0; index < entries; index += 1) {
        const entry = ifdStart + 2 + index * 12;
        if (entry + 12 > segmentEnd) return 1;
        if (bytes.getUint16(entry, littleEndian) !== 0x0112) continue;
        const type = bytes.getUint16(entry + 2, littleEndian);
        const count = bytes.getUint32(entry + 4, littleEndian);
        if (type !== 3 || count !== 1) return 1;
        const orientation = bytes.getUint16(entry + 8, littleEndian);
        return orientation >= 1 && orientation <= 8 ? orientation : 1;
      }
      return 1;
    }
    offset = segmentEnd;
  }
  return 1;
};

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

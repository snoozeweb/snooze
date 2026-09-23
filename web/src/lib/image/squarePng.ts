// Turns a picked image file into the profile picture the server stores: the
// centred square of it, scaled to AVATAR_PIXELS, re-encoded as PNG.
//
// Done in the browser so the upload is small and predictable — the server
// caps decoded input at 512 KiB and 512×512, so a phone photo sent as-is would
// be refused — and so every picture arrives in the one format the server
// wants. The server re-encodes it anyway (metadata stripped, polyglots
// neutralised); this is about size and shape, not trust.

/** The stored picture's side, in pixels. */
export const AVATAR_PIXELS = 128;
/** What the file picker accepts. WebP is decoded here; only PNG leaves. */
export const AVATAR_INPUT_TYPES = ["image/png", "image/jpeg", "image/webp"] as const;
/** Refuse bigger files before decoding them: nobody needs a 40 MB portrait,
 *  and decoding one can stall a low-end phone. */
export const AVATAR_MAX_INPUT_BYTES = 5 * 1024 * 1024;

/** The largest centred square inside a `width`×`height` image. */
export function centreSquare(
  width: number,
  height: number,
): { sx: number; sy: number; side: number } {
  const side = Math.min(width, height);
  return {
    sx: Math.floor((width - side) / 2),
    sy: Math.floor((height - side) / 2),
    side,
  };
}

/** Why a file cannot be used, or null when it can. */
export function checkAvatarFile(file: Pick<File, "type" | "size">): string | null {
  if (!(AVATAR_INPUT_TYPES as readonly string[]).includes(file.type)) {
    return "Pick a PNG, JPEG or WebP image.";
  }
  if (file.size > AVATAR_MAX_INPUT_BYTES) {
    return "That image is over 5 MB — pick a smaller one.";
  }
  return null;
}

type Decoded = { source: CanvasImageSource; width: number; height: number; release: () => void };

async function decode(file: Blob): Promise<Decoded> {
  if (typeof createImageBitmap === "function") {
    const bmp = await createImageBitmap(file);
    return { source: bmp, width: bmp.width, height: bmp.height, release: () => bmp.close() };
  }
  // Older Safari: an <img> over an object URL.
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    await new Promise<void>((resolve, reject) => {
      img.onload = () => resolve();
      img.onerror = () => reject(new Error("decode"));
      img.src = url;
    });
    return {
      source: img,
      width: img.naturalWidth,
      height: img.naturalHeight,
      release: () => URL.revokeObjectURL(url),
    };
  } catch (err) {
    URL.revokeObjectURL(url);
    throw err;
  }
}

/**
 * squarePngDataUrl decodes `file`, crops its centred square, scales it to
 * `size`×`size` and returns a `data:image/png;base64,…` URL. Rejects with a
 * readable Error when the file is not a decodable image.
 */
export async function squarePngDataUrl(file: Blob, size = AVATAR_PIXELS): Promise<string> {
  let decoded: Decoded;
  try {
    decoded = await decode(file);
  } catch {
    throw new Error("That file couldn't be read as an image.");
  }
  try {
    if (decoded.width <= 0 || decoded.height <= 0) {
      throw new Error("That file couldn't be read as an image.");
    }
    const { sx, sy, side } = centreSquare(decoded.width, decoded.height);
    const canvas = document.createElement("canvas");
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("This browser can't resize images.");
    ctx.imageSmoothingEnabled = true;
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(decoded.source, sx, sy, side, side, 0, 0, size, size);
    return canvas.toDataURL("image/png");
  } finally {
    decoded.release();
  }
}

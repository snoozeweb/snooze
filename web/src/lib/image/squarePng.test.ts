import { afterEach, describe, expect, it, vi } from "vitest";
import {
  AVATAR_MAX_INPUT_BYTES,
  AVATAR_PIXELS,
  centreSquare,
  checkAvatarFile,
  squarePngDataUrl,
} from "./squarePng";

describe("centreSquare", () => {
  it("takes the middle of a landscape image", () => {
    expect(centreSquare(300, 200)).toEqual({ sx: 50, sy: 0, side: 200 });
  });
  it("takes the middle of a portrait image", () => {
    expect(centreSquare(200, 301)).toEqual({ sx: 0, sy: 50, side: 200 });
  });
  it("leaves a square alone", () => {
    expect(centreSquare(64, 64)).toEqual({ sx: 0, sy: 0, side: 64 });
  });
});

describe("checkAvatarFile", () => {
  it("accepts PNG, JPEG and WebP under the limit", () => {
    for (const type of ["image/png", "image/jpeg", "image/webp"]) {
      expect(checkAvatarFile({ type, size: 1000 })).toBeNull();
    }
  });
  it("refuses other types — SVG above all", () => {
    expect(checkAvatarFile({ type: "image/svg+xml", size: 10 })).toMatch(/PNG, JPEG or WebP/);
    expect(checkAvatarFile({ type: "application/pdf", size: 10 })).toMatch(/PNG, JPEG or WebP/);
  });
  it("refuses files over 5 MB before decoding them", () => {
    expect(checkAvatarFile({ type: "image/png", size: AVATAR_MAX_INPUT_BYTES + 1 })).toMatch(
      /5 MB/,
    );
  });
});

describe("squarePngDataUrl", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("draws the centred square onto a 128px canvas and exports PNG", async () => {
    const close = vi.fn();
    vi.stubGlobal(
      "createImageBitmap",
      vi.fn(() => Promise.resolve({ width: 400, height: 300, close })),
    );
    const drawImage = vi.fn();
    const getContext = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      .mockReturnValue({ drawImage } as unknown as CanvasRenderingContext2D);
    const toDataURL = vi
      .spyOn(HTMLCanvasElement.prototype, "toDataURL")
      .mockReturnValue("data:image/png;base64,AAAA");
    try {
      const out = await squarePngDataUrl(new Blob(["x"], { type: "image/jpeg" }));
      expect(out).toBe("data:image/png;base64,AAAA");
      expect(drawImage).toHaveBeenCalledWith(
        expect.anything(),
        50,
        0,
        300,
        300,
        0,
        0,
        AVATAR_PIXELS,
        AVATAR_PIXELS,
      );
      expect(toDataURL).toHaveBeenCalledWith("image/png");
      expect(close).toHaveBeenCalled();
    } finally {
      getContext.mockRestore();
      toDataURL.mockRestore();
    }
  });

  it("rejects with a readable message when the file will not decode", async () => {
    vi.stubGlobal(
      "createImageBitmap",
      vi.fn(() => Promise.reject(new Error("bad"))),
    );
    await expect(squarePngDataUrl(new Blob(["nope"]))).rejects.toThrow(/couldn't be read/);
  });
});

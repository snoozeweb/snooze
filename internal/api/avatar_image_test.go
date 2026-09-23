package api

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// solidImage returns a w×h image filled with one colour: it compresses to a
// few hundred bytes, so it only trips the dimension checks.
func solidImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 80, B: 40, A: 255})
		}
	}
	return img
}

// noiseImage returns a w×h image of seeded random pixels, which no lossless
// codec can shrink — used to trip the byte-size limits.
func noiseImage(w, h int) *image.RGBA {
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic test fixture
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	_, _ = rng.Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}))
	return buf.Bytes()
}

func dataURL(mime string, raw []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

// pngWithHeaderDims rewrites the IHDR of a real 1×1 PNG so it claims w×h. The
// pixel data stays tiny, so only a header-first check can reject it without
// allocating the claimed frame.
func pngWithHeaderDims(t *testing.T, w, h uint32) []byte {
	t.Helper()
	raw := encodePNG(t, solidImage(1, 1))
	// 8-byte signature, 4-byte length, "IHDR", then width/height.
	require.Equal(t, "IHDR", string(raw[12:16]))
	binary.BigEndian.PutUint32(raw[16:20], w)
	binary.BigEndian.PutUint32(raw[20:24], h)
	// IHDR data is 13 bytes; its CRC covers type + data.
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	return raw
}

func TestNormalizeAvatar(t *testing.T) {
	t.Parallel()

	noisyJPEG := encodeJPEG(t, noiseImage(400, 400), 90)
	require.Less(t, len(noisyJPEG), avatarMaxInputBytes, "fixture must pass the input limit")
	bigPNG := encodePNG(t, noiseImage(512, 512))
	require.Greater(t, len(bigPNG), avatarMaxInputBytes, "fixture must exceed the input limit")

	cases := []struct {
		name    string
		in      string
		wantErr error // nil = accepted
		wantW   int
		wantH   int
	}{
		{"png", dataURL("image/png", encodePNG(t, solidImage(128, 128))), nil, 128, 128},
		{"jpeg", dataURL("image/jpeg", encodeJPEG(t, solidImage(64, 32), 90)), nil, 64, 32},
		{"jpg alias", dataURL("image/jpg", encodeJPEG(t, solidImage(32, 32), 90)), nil, 32, 32},
		{"max size square", dataURL("image/png", encodePNG(t, solidImage(512, 512))), nil, 512, 512},
		{"aspect exactly 2:1", dataURL("image/png", encodePNG(t, solidImage(100, 50))), nil, 100, 50},
		{"svg", dataURL("image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)), errAvatarInvalid, 0, 0},
		{"svg labelled png", dataURL("image/png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)), errAvatarInvalid, 0, 0},
		{"garbage", dataURL("image/png", []byte("definitely not an image")), errAvatarInvalid, 0, 0},
		{"truncated png", dataURL("image/png", encodePNG(t, solidImage(16, 16))[:40]), errAvatarInvalid, 0, 0},
		{"jpeg labelled png", dataURL("image/png", encodeJPEG(t, solidImage(16, 16), 90)), errAvatarInvalid, 0, 0},
		{"gif", dataURL("image/gif", []byte("GIF89a\x01\x00\x01\x00")), errAvatarInvalid, 0, 0},
		{"not a data url", "https://example.com/a.png", errAvatarInvalid, 0, 0},
		{"not base64", "data:image/png,rawbytes", errAvatarInvalid, 0, 0},
		{"bad base64", "data:image/png;base64,!!!", errAvatarInvalid, 0, 0},
		{"empty", "", errAvatarInvalid, 0, 0},
		{"too tall", dataURL("image/png", encodePNG(t, solidImage(100, 300))), errAvatarInvalid, 0, 0},
		{"too wide", dataURL("image/png", encodePNG(t, solidImage(201, 100))), errAvatarInvalid, 0, 0},
		{"over 512 px", dataURL("image/png", encodePNG(t, solidImage(513, 513))), errAvatarInvalid, 0, 0},
		{"huge header dims", dataURL("image/png", pngWithHeaderDims(t, 100000, 100000)), errAvatarInvalid, 0, 0},
		{"zero header dims", dataURL("image/png", pngWithHeaderDims(t, 0, 0)), errAvatarInvalid, 0, 0},
		{"input too big", dataURL("image/png", bigPNG), errAvatarTooLarge, 0, 0},
		{"re-encoded too big", dataURL("image/jpeg", noisyJPEG), errAvatarTooLarge, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := normalizeAvatar(tc.in)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.LessOrEqual(t, len(out), avatarMaxStoredBytes)
			// Always re-encoded to PNG, whatever came in.
			cfg, err := png.DecodeConfig(bytes.NewReader(out))
			require.NoError(t, err)
			require.Equal(t, tc.wantW, cfg.Width)
			require.Equal(t, tc.wantH, cfg.Height)
		})
	}
}

// TestNormalizeAvatar_DropsTrailingPayload proves the re-encode neutralises a
// polyglot: bytes appended after the PNG's IEND do not survive.
func TestNormalizeAvatar_DropsTrailingPayload(t *testing.T) {
	t.Parallel()
	raw := append(encodePNG(t, solidImage(8, 8)), []byte("<script>alert(1)</script>")...)
	out, err := normalizeAvatar(dataURL("image/png", raw))
	require.NoError(t, err)
	require.NotContains(t, string(out), "<script>")
}

func TestAvatarVersion(t *testing.T) {
	t.Parallel()
	a := avatarVersion([]byte("a"))
	require.Len(t, a, 16)
	require.Equal(t, a, avatarVersion([]byte("a")), "version is a pure content hash")
	require.NotEqual(t, a, avatarVersion([]byte("b")))
}

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
)

// Profile-picture limits, matching PUT /api/v1/user/me/avatar in
// api/openapi.yaml.
const (
	// avatarMaxInputBytes caps the decoded upload (before re-encoding).
	avatarMaxInputBytes = 512 << 10
	// avatarMaxStoredBytes caps the re-encoded PNG that is actually stored.
	avatarMaxStoredBytes = 128 << 10
	// avatarMaxSide caps each dimension, in pixels.
	avatarMaxSide = 512
	// avatarMaxAspect caps the long side over the short side.
	avatarMaxAspect = 2
)

var (
	// errAvatarInvalid marks an upload that is not a decodable PNG/JPEG data
	// URL or whose dimensions are out of bounds (422).
	errAvatarInvalid = errors.New("invalid avatar")
	// errAvatarTooLarge marks an upload over a byte limit, before or after
	// re-encoding (413).
	errAvatarTooLarge = errors.New("avatar too large")
)

var (
	pngMagic  = []byte("\x89PNG\r\n\x1a\n")
	jpegMagic = []byte{0xff, 0xd8, 0xff}
)

// normalizeAvatar validates a `data:image/png|jpeg;base64,…` URL and returns
// the picture re-encoded as PNG. The format is decided by the content's magic
// bytes, not the declared type, and the two must agree; SVG (or anything else)
// is never accepted. Dimensions are checked from the header before the pixels
// are decoded, so a small file claiming a huge frame is refused without
// allocating it. Re-encoding drops every ancillary chunk (EXIF, text, trailing
// bytes), which is what neutralises polyglot uploads.
func normalizeAvatar(url string) ([]byte, error) {
	rest, ok := strings.CutPrefix(url, "data:")
	if !ok {
		return nil, fmt.Errorf("%w: not a data: URL", errAvatarInvalid)
	}
	header, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return nil, fmt.Errorf("%w: malformed data: URL", errAvatarInvalid)
	}
	mime, ok := strings.CutSuffix(header, ";base64")
	if !ok {
		return nil, fmt.Errorf("%w: data: URL must be base64-encoded", errAvatarInvalid)
	}
	var declared string
	switch strings.ToLower(mime) {
	case "image/png":
		declared = "png"
	case "image/jpeg", "image/jpg":
		declared = "jpeg"
	default:
		return nil, fmt.Errorf("%w: only image/png and image/jpeg are accepted", errAvatarInvalid)
	}
	// Refuse on the encoded length before decoding anything.
	if len(payload) > base64.StdEncoding.EncodedLen(avatarMaxInputBytes) {
		return nil, fmt.Errorf("%w: over %d KiB", errAvatarTooLarge, avatarMaxInputBytes>>10)
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		if raw, err = base64.RawStdEncoding.DecodeString(payload); err != nil {
			return nil, fmt.Errorf("%w: bad base64", errAvatarInvalid)
		}
	}
	if len(raw) > avatarMaxInputBytes {
		return nil, fmt.Errorf("%w: over %d KiB", errAvatarTooLarge, avatarMaxInputBytes>>10)
	}

	var (
		decodeConfig func(*bytes.Reader) (image.Config, error)
		decode       func(*bytes.Reader) (image.Image, error)
		sniffed      string
	)
	switch {
	case bytes.HasPrefix(raw, pngMagic):
		sniffed = "png"
		decodeConfig = func(r *bytes.Reader) (image.Config, error) { return png.DecodeConfig(r) }
		decode = func(r *bytes.Reader) (image.Image, error) { return png.Decode(r) }
	case bytes.HasPrefix(raw, jpegMagic):
		sniffed = "jpeg"
		decodeConfig = func(r *bytes.Reader) (image.Config, error) { return jpeg.DecodeConfig(r) }
		decode = func(r *bytes.Reader) (image.Image, error) { return jpeg.Decode(r) }
	default:
		return nil, fmt.Errorf("%w: content is not a PNG or JPEG image", errAvatarInvalid)
	}
	if sniffed != declared {
		return nil, fmt.Errorf("%w: declared %s but content is %s", errAvatarInvalid, declared, sniffed)
	}

	cfg, err := decodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAvatarInvalid, err)
	}
	if err := checkAvatarDims(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	img, err := decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAvatarInvalid, err)
	}

	var out bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("%w: re-encode: %v", errAvatarInvalid, err)
	}
	if out.Len() > avatarMaxStoredBytes {
		return nil, fmt.Errorf("%w: %d KiB after re-encoding to PNG, limit %d KiB",
			errAvatarTooLarge, out.Len()>>10, avatarMaxStoredBytes>>10)
	}
	return out.Bytes(), nil
}

// checkAvatarDims enforces the pixel bounds: both sides in 1..avatarMaxSide
// and the long side at most avatarMaxAspect times the short one.
func checkAvatarDims(w, h int) error {
	if w <= 0 || h <= 0 || w > avatarMaxSide || h > avatarMaxSide {
		return fmt.Errorf("%w: %d×%d is outside 1..%d pixels per side", errAvatarInvalid, w, h, avatarMaxSide)
	}
	long, short := max(w, h), min(w, h)
	if long > avatarMaxAspect*short {
		return fmt.Errorf("%w: %d×%d is more elongated than %d:1", errAvatarInvalid, w, h, avatarMaxAspect)
	}
	return nil
}

// avatarVersion is the content hash clients cache a picture by: the first 16
// hex characters of the SHA-256 of the stored PNG.
func avatarVersion(pngBytes []byte) string {
	sum := sha256.Sum256(pngBytes)
	return hex.EncodeToString(sum[:])[:16]
}

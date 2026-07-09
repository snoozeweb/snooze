import type { BrandName } from "./brand-names";
import { maskUrlFor } from "./brand-names";

export type BrandIconProps = {
  name: BrandName;
  size?: 12 | 14 | 16 | 20 | 24;
  label?: string;
  className?: string;
};

// Renders a vendored brand glyph. Two shapes are supported, both monochrome and
// tinted with `currentColor` so the mark inherits the surrounding text color
// (and tints to the accent on hover, like the rest of the UI):
//
//   - Sprite brands (Simple Icons in web/public/brands.svg): fill-based SVG
//     symbols painted via `<use>` with `fill: currentColor`.
//   - Mask brands (a single-color PNG silhouette, e.g. the Snooze bell): the
//     PNG's alpha channel is used as a CSS mask over a `currentColor` fill.
//
// Either way the brand SHAPE carries the recognition; we don't hard-code brand
// colors, keeping dark/light theming intact.
export function BrandIcon({ name, size = 16, label, className }: BrandIconProps) {
  const labelled = label != null;
  const maskUrl = maskUrlFor(name);

  if (maskUrl) {
    return (
      <span
        className={className}
        role={labelled ? "img" : undefined}
        aria-hidden={labelled ? undefined : true}
        aria-label={labelled ? label : undefined}
        style={{
          display: "inline-block",
          width: size,
          height: size,
          backgroundColor: "currentColor",
          maskImage: `url("${maskUrl}")`,
          WebkitMaskImage: `url("${maskUrl}")`,
          maskRepeat: "no-repeat",
          WebkitMaskRepeat: "no-repeat",
          maskPosition: "center",
          WebkitMaskPosition: "center",
          maskSize: "contain",
          WebkitMaskSize: "contain",
        }}
      />
    );
  }

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      className={className}
      aria-hidden={labelled ? undefined : true}
      role={labelled ? "img" : undefined}
      aria-label={labelled ? label : undefined}
    >
      <use href={`/web/brands.svg#brand-${name}`} />
    </svg>
  );
}

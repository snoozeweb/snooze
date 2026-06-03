import type { BrandName } from "./brand-names";

export type BrandIconProps = {
  name: BrandName;
  size?: 12 | 14 | 16 | 20 | 24;
  label?: string;
  className?: string;
};

// Renders a vendored brand glyph from web/public/brands.svg. Unlike Icon
// (stroke-based Lucide line icons), Simple Icons brand marks are fill-based, so
// we paint with `fill: currentColor` and no stroke — the glyph inherits the
// surrounding text color (and tints to the accent on hover, like the rest of
// the UI). The brand SHAPE carries the recognition; we don't hard-code brand
// colors, keeping dark/light theming intact.
export function BrandIcon({ name, size = 16, label, className }: BrandIconProps) {
  const labelled = label != null;
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

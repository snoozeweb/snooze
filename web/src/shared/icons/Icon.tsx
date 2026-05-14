import type { IconName } from "./icon-names";

export type IconProps = {
  name: IconName;
  size?: 12 | 14 | 16 | 20 | 24;
  label?: string;
  className?: string;
};

export function Icon({ name, size = 16, label, className }: IconProps) {
  const labelled = label != null;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden={labelled ? undefined : true}
      role={labelled ? "img" : undefined}
      aria-label={labelled ? label : undefined}
    >
      <use href={`/web/icons.svg#icon-${name}`} />
    </svg>
  );
}

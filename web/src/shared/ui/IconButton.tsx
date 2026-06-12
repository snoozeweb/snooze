import type { ButtonHTMLAttributes } from "react";
import type React from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import type { ButtonSize, ButtonVariant } from "./Button";
import styles from "./IconButton.module.css";

export type IconButtonProps = Omit<
  ButtonHTMLAttributes<HTMLButtonElement>,
  "type" | "aria-label"
> & {
  icon: IconName;
  label: string;
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
  type?: "button" | "submit" | "reset";
  ref?: React.Ref<HTMLButtonElement>;
};

export function IconButton({
  icon,
  label,
  variant = "ghost",
  size = "md",
  loading,
  type = "button",
  disabled,
  className,
  ref,
  ...rest
}: IconButtonProps) {
  const classes = [styles.iconButton, styles[size], styles[variant], className]
    .filter(Boolean)
    .join(" ");
  const iconSize = size === "lg" ? 20 : 16;
  return (
    <button
      ref={ref}
      type={type}
      className={classes}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      aria-label={label}
      title={label}
      {...rest}
    >
      <Icon name={icon} size={iconSize} />
    </button>
  );
}

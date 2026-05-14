import { forwardRef } from "react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import { Spinner } from "./Spinner";
import styles from "./Button.module.css";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
export type ButtonSize = "sm" | "md" | "lg";

export type ButtonProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "type"> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
  leadingIcon?: IconName;
  trailingIcon?: IconName;
  fullWidth?: boolean;
  type?: "button" | "submit" | "reset";
  children?: ReactNode;
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    variant = "secondary",
    size = "md",
    loading = false,
    leadingIcon,
    trailingIcon,
    fullWidth = false,
    type = "button",
    disabled,
    className,
    children,
    ...rest
  },
  ref,
) {
  const classes = [
    styles.button,
    styles[size],
    styles[variant],
    styles.relative,
    fullWidth ? styles.fullWidth : null,
    className,
  ]
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
      {...rest}
    >
      <span
        className={[styles.content, loading ? styles.contentHidden : null]
          .filter(Boolean)
          .join(" ")}
      >
        {leadingIcon ? <Icon name={leadingIcon} size={iconSize} /> : null}
        {children}
        {trailingIcon ? <Icon name={trailingIcon} size={iconSize} /> : null}
      </span>
      {loading ? (
        <span className={styles.spinner}>
          <Spinner size={iconSize === 20 ? 20 : 16} />
        </span>
      ) : null}
    </button>
  );
});

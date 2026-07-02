import type { ButtonHTMLAttributes } from "react";
import type React from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import type { ButtonSize, ButtonVariant } from "./Button";
import { Tooltip, TooltipProvider } from "./Tooltip";
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
  /**
   * The app Tooltip is rendered by default (using `label` as its content) so
   * every icon-only button gets the same styled, 200ms hover hint instead of
   * the browser's native `title` bubble. Set to `false` ONLY when the button is
   * itself a Radix trigger child (Menu/Popover `asChild`): a nested
   * `Tooltip.Root` would swallow the trigger's props/ref, so the parent
   * composes the Tooltip around the trigger instead (see RowActionsMenu). The
   * `aria-label` is always present, so accessibility is unaffected either way.
   */
  withTooltip?: boolean;
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
  withTooltip = true,
  ...rest
}: IconButtonProps) {
  const classes = [styles.iconButton, styles[size], styles[variant], className]
    .filter(Boolean)
    .join(" ");
  const iconSize = size === "lg" ? 20 : 16;
  const isDisabled = disabled || loading;
  const button = (
    <button
      ref={ref}
      type={type}
      className={classes}
      disabled={isDisabled}
      aria-busy={loading || undefined}
      aria-label={label}
      // A disabled button emits no pointer/focus events, so the styled Tooltip
      // below never opens — keep the native `title` as the only remaining hover
      // hint in that state. Enabled buttons get the styled Tooltip instead.
      {...(isDisabled ? { title: label } : {})}
      {...rest}
    >
      <Icon name={icon} size={iconSize} />
    </button>
  );
  // The styled Tooltip replaces the native `title` for the common case. When
  // opted out (Radix trigger composition) we return the bare button so the
  // parent's asChild Slot can forward props and refs onto it unimpeded.
  if (!withTooltip || !label) return button;
  // Radix Tooltip throws without a TooltipProvider ancestor. IconButton is used
  // in ~30 places, many under bare test harnesses (and any future provider-less
  // mount), so we carry a local provider fallback. In the real app the root
  // provider already wraps everything; this nested provider is harmless and
  // keeps the same 200ms delay, so behaviour is unchanged either way.
  return (
    <TooltipProvider delay={200}>
      <Tooltip content={label}>{button}</Tooltip>
    </TooltipProvider>
  );
}

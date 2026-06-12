import type { InputHTMLAttributes } from "react";
import type React from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import styles from "./Input.module.css";

export type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, "size"> & {
  invalid?: boolean;
  size?: "sm" | "md" | "lg";
  leadingIcon?: IconName;
  trailingIcon?: IconName;
  ref?: React.Ref<HTMLInputElement>;
};

export function Input({
  invalid,
  size = "md",
  leadingIcon,
  trailingIcon,
  className,
  ref,
  ...rest
}: InputProps) {
  const wrapClasses = [styles.wrap, styles[size], invalid ? styles.invalid : null, className]
    .filter(Boolean)
    .join(" ");
  return (
    <div className={wrapClasses}>
      {leadingIcon ? (
        <span className={styles.iconLeft}>
          <Icon name={leadingIcon} size={14} />
        </span>
      ) : null}
      <input
        ref={ref}
        className={styles.input}
        {...(invalid ? { "aria-invalid": true } : {})}
        {...rest}
      />
      {trailingIcon ? (
        <span className={styles.iconRight}>
          <Icon name={trailingIcon} size={14} />
        </span>
      ) : null}
    </div>
  );
}

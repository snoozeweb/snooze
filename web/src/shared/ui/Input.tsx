import { forwardRef } from "react";
import type { InputHTMLAttributes } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import styles from "./Input.module.css";

export type InputProps = InputHTMLAttributes<HTMLInputElement> & {
  invalid?: boolean;
  size?: "sm" | "md" | "lg";
  leadingIcon?: IconName;
  trailingIcon?: IconName;
};

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { invalid, size = "md", leadingIcon, trailingIcon, className, ...rest },
  ref,
) {
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
});

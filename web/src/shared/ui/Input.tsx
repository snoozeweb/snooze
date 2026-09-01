import { useId, type InputHTMLAttributes } from "react";
import type React from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import styles from "./Input.module.css";

export type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, "size"> & {
  invalid?: boolean;
  /** Visible validation message shown under the field when `invalid` is
   *  true, and wired to the input via `aria-describedby` — an `aria-invalid`
   *  with no paired message leaves both sighted and screen-reader users
   *  stranded on a red border with no idea what to fix. Pass the SAME message
   *  that already gates the submit (don't invent a new validation rule). */
  errorMessage?: string | undefined;
  size?: "sm" | "md" | "lg";
  leadingIcon?: IconName;
  trailingIcon?: IconName;
  ref?: React.Ref<HTMLInputElement>;
};

export function Input({
  invalid,
  errorMessage,
  size = "md",
  leadingIcon,
  trailingIcon,
  className,
  ref,
  id,
  "aria-describedby": describedBy,
  ...rest
}: InputProps) {
  const generatedId = useId();
  const errorId = `${id ?? generatedId}-error`;
  const showError = invalid && !!errorMessage;
  const wrapClasses = [
    styles.wrap,
    styles[size],
    invalid ? styles.invalid : null,
    rest.disabled ? styles.disabledWrap : null,
    className,
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <>
      <div className={wrapClasses}>
        {leadingIcon ? (
          <span className={styles.iconLeft}>
            <Icon name={leadingIcon} size={14} />
          </span>
        ) : null}
        <input
          ref={ref}
          id={id}
          className={styles.input}
          {...(invalid ? { "aria-invalid": true } : {})}
          aria-describedby={
            [describedBy, showError ? errorId : undefined].filter(Boolean).join(" ") || undefined
          }
          {...rest}
        />
        {trailingIcon ? (
          <span className={styles.iconRight}>
            <Icon name={trailingIcon} size={14} />
          </span>
        ) : null}
      </div>
      {showError ? (
        <p id={errorId} role="alert" className={styles.errorMessage}>
          {errorMessage}
        </p>
      ) : null}
    </>
  );
}

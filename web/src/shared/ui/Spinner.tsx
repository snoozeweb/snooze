import styles from "./Spinner.module.css";

export type SpinnerProps = {
  size?: 12 | 16 | 20;
  className?: string;
  label?: string;
};

export function Spinner({ size = 16, className, label = "Loading" }: SpinnerProps) {
  const classes = [styles.spinner, className].filter(Boolean).join(" ");
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      role="status"
      aria-label={label}
      className={classes}
    >
      <circle className={styles.track} cx="12" cy="12" r="10" fill="none" stroke="currentColor" strokeWidth="2" />
      <circle className={styles.head} cx="12" cy="12" r="10" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}

import { useTheme } from "@/shared/hooks/useTheme";
import styles from "./Logo.module.css";

export type LogoProps = {
  className?: string;
  alt?: string;
};

export function Logo({ className, alt = "Snooze" }: LogoProps) {
  const { theme } = useTheme();
  const src = theme === "dark" ? "/web/logo_white.png" : "/web/logo.png";
  return (
    <img
      src={src}
      alt={alt}
      className={`${styles.logo} ${className ?? ""}`.trim()}
      draggable={false}
    />
  );
}

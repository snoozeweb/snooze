import type { ReactNode } from "react";
import { Icon } from "@/shared/icons/Icon";
import { personLabel, useAvatarImage, usePerson } from "@/shared/people/api";
import { avatarTone, initialsOf } from "./avatarUtils";
import { Tooltip } from "./Tooltip";
import styles from "./Avatar.module.css";

export type AvatarVariant = "normal" | "ghost" | "bot";
export type AvatarSize = "sm" | "md" | "lg";

export type AvatarProps = {
  /** Login. Empty for a system actor (`variant="bot"`). */
  name: string;
  /** Auth method, when the caller knows it — picks the right directory entry
   *  and addresses the picture. Falls back to the directory's. */
  method?: string | undefined;
  /** "ghost" is a previous owner (faded, dashed ring); "bot" an automated
   *  actor (a glyph instead of a face). */
  variant?: AvatarVariant;
  size?: AvatarSize;
  /** Accessible name. Defaults to the person's display name, else login. */
  label?: string | undefined;
  /** Tooltip content; defaults to the label. `false` renders none. Needs a
   *  TooltipProvider above it (the app root has one). */
  tooltip?: ReactNode | false;
  /** Pure decoration next to a name that is already on screen: hidden from
   *  assistive tech and never tooltipped. */
  decorative?: boolean;
  /** Paint this `data:` URL instead of the stored picture — the profile
   *  page's preview before an upload is saved. */
  src?: string | undefined;
  className?: string | undefined;
};

/**
 * Avatar — a person's face: their uploaded picture when the directory says
 * they have one, their initials on a hashed colour otherwise.
 *
 * The picture comes through the authenticated API client as a `data:` URL
 * (see useAvatarImage), cached per (method, name, version), so a table of 50
 * rows owned by three people costs three requests, once.
 */
export function Avatar({
  name,
  method,
  variant = "normal",
  size = "md",
  label,
  tooltip,
  decorative = false,
  src,
  className,
}: AvatarProps) {
  const isBot = variant === "bot";
  const person = usePerson(isBot ? "" : name, method);
  const image = useAvatarImage(
    method || person?.method,
    isBot || src ? "" : name,
    person?.avatar_version,
  );
  const shown = personLabel(person, name);
  const accessibleName = label ?? (isBot ? name || "System" : shown);
  const picture = src ?? image.data ?? null;

  const body = (
    <span
      className={[styles.avatar, className].filter(Boolean).join(" ")}
      data-size={size}
      data-variant={variant}
      data-tone={isBot || picture ? undefined : avatarTone(name)}
      {...(decorative ? { "aria-hidden": true } : { role: "img", "aria-label": accessibleName })}
    >
      {isBot ? (
        <Icon name="zap" size={size === "lg" ? 24 : 12} />
      ) : picture ? (
        <img className={styles.img} src={picture} alt="" draggable={false} />
      ) : (
        <span className={styles.initials}>{initialsOf(shown)}</span>
      )}
    </span>
  );

  if (decorative || tooltip === false) return body;
  return <Tooltip content={tooltip ?? accessibleName}>{body}</Tooltip>;
}

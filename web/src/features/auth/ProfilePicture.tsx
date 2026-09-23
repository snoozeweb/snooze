// ProfilePicture — the Profile page's picture section: the current face, a
// file picker that crops and shrinks in the browser, a preview to accept or
// throw away, and Remove to go back to initials.
//
// Saving and removing invalidate the people directory and every cached
// picture (see shared/people/api.ts), so the sidebar, the Owner column and
// the timeline repaint without a reload.
import { useRef, useState } from "react";
import { Avatar } from "@/shared/ui/Avatar";
import { Button } from "@/shared/ui/Button";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { describeError } from "@/lib/api/errorMessage";
import { AVATAR_INPUT_TYPES, checkAvatarFile, squarePngDataUrl } from "@/lib/image/squarePng";
import { usePerson, useRemoveAvatar, useUploadAvatar } from "@/shared/people/api";
import styles from "./ProfilePicture.module.css";

/** The server's two refusals, in words an operator can act on. */
function uploadErrorCopy(err: unknown): string {
  if (err instanceof ApiError && err.status === 413) {
    return "That picture is too large for the server. Try a smaller or simpler image.";
  }
  if (err instanceof ApiError && err.status === 422) {
    return "The server couldn't use that picture. Try a different PNG or JPEG.";
  }
  return describeError(err, "Couldn't save the picture.").summary;
}

export function ProfilePicture({ name, method }: { name: string; method?: string | undefined }) {
  const person = usePerson(name, method);
  const hasPicture = !!person?.avatar_version;
  const upload = useUploadAvatar();
  const remove = useRemoveAvatar();
  const inputRef = useRef<HTMLInputElement>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [preparing, setPreparing] = useState(false);

  async function handleFile(file: File | undefined) {
    setError(null);
    if (!file) return;
    const problem = checkAvatarFile(file);
    if (problem) {
      setError(problem);
      return;
    }
    setPreparing(true);
    try {
      setPreview(await squarePngDataUrl(file));
    } catch (e) {
      setError(e instanceof Error ? e.message : "That file couldn't be read as an image.");
    } finally {
      setPreparing(false);
    }
  }

  async function save() {
    if (!preview) return;
    setError(null);
    try {
      await upload.mutateAsync(preview);
      setPreview(null);
      toast.success("Profile picture updated");
    } catch (e) {
      setError(uploadErrorCopy(e));
    }
  }

  async function removePicture() {
    setError(null);
    try {
      await remove.mutateAsync();
      toast.success("Profile picture removed");
    } catch (e) {
      setError(describeError(e, "Couldn't remove the picture.").summary);
    }
  }

  return (
    <section className={styles.section} aria-labelledby="profile-picture-title">
      <h2 id="profile-picture-title" className={styles.title}>
        Profile picture
      </h2>
      <div className={styles.body}>
        <Avatar
          name={name}
          method={method}
          size="lg"
          src={preview ?? undefined}
          label={preview ? "Preview of your new profile picture" : "Your profile picture"}
          tooltip={false}
        />
        <div className={styles.controls}>
          <p className={styles.hint}>
            {preview
              ? "This is how it will look. Save it, or pick another."
              : "Shown wherever you appear — the alerts you own, the timeline, the sidebar. A PNG, JPEG or WebP; it is cropped to a centred square."}
          </p>
          <input
            ref={inputRef}
            id="profile-picture-file"
            className={styles.file}
            type="file"
            accept={AVATAR_INPUT_TYPES.join(",")}
            aria-label="Choose a profile picture"
            tabIndex={-1}
            onChange={(e) => {
              const file = e.target.files?.[0];
              // Reset so picking the same file again still fires a change.
              e.target.value = "";
              void handleFile(file);
            }}
          />
          <div className={styles.actions}>
            <Button
              size="sm"
              variant={preview ? "secondary" : "primary"}
              leadingIcon="upload"
              loading={preparing}
              onClick={() => inputRef.current?.click()}
            >
              {preview ? "Pick another" : hasPicture ? "Change picture" : "Upload picture"}
            </Button>
            {preview ? (
              <>
                <Button
                  size="sm"
                  variant="primary"
                  leadingIcon="save"
                  loading={upload.isPending}
                  onClick={() => void save()}
                >
                  Save picture
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setPreview(null);
                    setError(null);
                  }}
                >
                  Cancel
                </Button>
              </>
            ) : hasPicture ? (
              // Not red: it only drops back to initials, and uploading again
              // undoes it.
              <Button
                size="sm"
                variant="ghost"
                leadingIcon="trash"
                loading={remove.isPending}
                onClick={() => void removePicture()}
              >
                Remove
              </Button>
            ) : null}
          </div>
          {error ? (
            <p className={styles.error} role="alert">
              {error}
            </p>
          ) : null}
        </div>
      </div>
    </section>
  );
}

package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// person is one entry of GET /api/v1/people.
type person struct {
	Name          string `json:"name"`
	Method        string `json:"method"`
	DisplayName   string `json:"display_name"`
	AvatarVersion string `json:"avatar_version"`
}

// avatarDoc is the GET /api/v1/avatar/{method}/{name} body; Data is a
// `data:image/png;base64,…` URL.
type avatarDoc struct {
	Name    string `json:"name"`
	Method  string `json:"method"`
	Version string `json:"version"`
	Data    string `json:"data"`
}

// maxAvatarFileBytes mirrors the server's decoded-input cap, so an oversized
// file is refused before it is read into memory and uploaded.
const maxAvatarFileBytes = 512 << 10

// newPeopleCmd implements `snooze people`: the tenant's user directory (login,
// auth method, display name, whether a profile picture is set) — the names
// `snooze record assign` accepts.
func newPeopleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "people",
		Short: "List the users of your tenant (who alerts can be assigned to)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			people, err := fetchPeople(cmd.Context(), cl)
			if err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				return renderAny(cmd, rt, people)
			}
			if len(people) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no users)")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			defer tw.Flush() //nolint:errcheck
			_, _ = fmt.Fprintln(tw, "name\tmethod\tdisplay_name\tpicture")
			for _, p := range people {
				picture := "no"
				if p.AvatarVersion != "" {
					picture = "yes"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, p.Method, p.DisplayName, picture)
			}
			return nil
		},
	}
}

// newAvatarCmd builds `snooze avatar {set,get,remove}`: the caller's own
// profile picture, and reading anyone's.
func newAvatarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "avatar",
		Short: "Manage profile pictures",
		Long: "Upload or remove your own profile picture, or download anyone's.\n" +
			"Pictures must be PNG or JPEG, at most 512x512 pixels and 512 KiB;\n" +
			"the server re-encodes them to PNG.",
	}
	cmd.AddCommand(newAvatarSetCmd(), newAvatarGetCmd(), newAvatarRemoveCmd())
	return cmd
}

func newAvatarSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <file.png|file.jpg>",
		Short: "Upload your profile picture",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dataURL, err := avatarDataURL(args[0])
			if err != nil {
				return err
			}
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			var resp struct {
				Version string `json:"version"`
			}
			if err := cl.Put(cmd.Context(), "/api/v1/user/me/avatar", map[string]any{"data": dataURL}, &resp); err != nil {
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				return renderAny(cmd, rt, resp)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Profile picture updated (version %s)\n", resp.Version)
			return nil
		},
	}
}

func newAvatarGetCmd() *cobra.Command {
	var userMethod, output string
	c := &cobra.Command{
		Use:   "get <user>",
		Short: "Download a user's profile picture",
		Long: "Download <user>'s profile picture. Without -o it prints the picture's\n" +
			"version and size; with -o <file> it writes the PNG.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			name, method := args[0], userMethod
			if method == "" {
				if method, err = resolvePersonMethod(cmd.Context(), cl, name); err != nil {
					return err
				}
			}
			var doc avatarDoc
			path := "/api/v1/avatar/" + url.PathEscape(method) + "/" + url.PathEscape(name)
			if err := cl.Get(cmd.Context(), path, &doc); err != nil {
				if apiErr, ok := snoozeclient.IsAPIError(err); ok && apiErr.Status == http.StatusNotFound {
					return fmt.Errorf("%s has no profile picture", name)
				}
				return err
			}
			if rt.flags != nil && rt.flags.JSON {
				return renderAny(cmd, rt, doc)
			}
			png, err := decodeAvatarDataURL(doc.Data)
			if err != nil {
				return err
			}
			if output == "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s (%s): version %s, %d bytes PNG — pass -o <file> to save it\n",
					name, method, doc.Version, len(png))
				return nil
			}
			if err := os.WriteFile(output, png, 0o600); err != nil {
				return fmt.Errorf("write %s: %w", output, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Saved %s's profile picture to %s\n", name, output)
			return nil
		},
	}
	c.Flags().StringVar(&userMethod, "user-method", "",
		"Auth method of <user>; looked up in the user directory when omitted")
	c.Flags().StringVarP(&output, "output", "o", "", "Write the PNG to this file")
	return c
}

func newAvatarRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Remove your profile picture (back to initials)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			cl, err := rt.buildClient()
			if err != nil {
				return err
			}
			if err := cl.Delete(cmd.Context(), "/api/v1/user/me/avatar", nil); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Profile picture removed")
			return nil
		},
	}
}

// fetchPeople reads the tenant's user directory.
func fetchPeople(ctx context.Context, cl *snoozeclient.Client) ([]person, error) {
	var resp struct {
		Data []person `json:"data"`
	}
	if err := cl.Get(ctx, "/api/v1/people", &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// resolvePersonMethod finds the auth method of login name in the directory.
// A login present under several methods is ambiguous and needs --user-method.
func resolvePersonMethod(ctx context.Context, cl *snoozeclient.Client, name string) (string, error) {
	people, err := fetchPeople(ctx, cl)
	if err != nil {
		return "", err
	}
	var methods []string
	for _, p := range people {
		if p.Name == name {
			methods = append(methods, p.Method)
		}
	}
	switch len(methods) {
	case 0:
		return "", fmt.Errorf("no user %q in your tenant (see `snooze people`)", name)
	case 1:
		return methods[0], nil
	default:
		return "", fmt.Errorf("user %q exists for several auth methods (%s); pass --user-method",
			name, strings.Join(methods, ", "))
	}
}

// avatarDataURL reads a picture file and wraps it in the data URL the upload
// endpoint takes. The type comes from the content, not the file extension, and
// only PNG and JPEG pass — the server decides the rest (dimensions, size after
// re-encoding).
func avatarDataURL(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxAvatarFileBytes {
		return "", fmt.Errorf("%s is %d KiB; profile pictures are limited to %d KiB",
			path, info.Size()>>10, maxAvatarFileBytes>>10)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // an operator-chosen local file
	if err != nil {
		return "", err
	}
	mime := http.DetectContentType(raw)
	if mime != "image/png" && mime != "image/jpeg" {
		return "", fmt.Errorf("%s is %s; profile pictures must be PNG or JPEG", path, mime)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// decodeAvatarDataURL extracts the PNG bytes from a `data:image/png;base64,…`
// URL.
func decodeAvatarDataURL(dataURL string) ([]byte, error) {
	_, payload, ok := strings.Cut(dataURL, ";base64,")
	if !ok || !strings.HasPrefix(dataURL, "data:") {
		return nil, errors.New("server returned a picture that is not a base64 data URL")
	}
	return base64.StdEncoding.DecodeString(payload)
}

package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newLoginCmd implements `snooze login`. It accepts username via --user, prompts
// for password if --password is empty, then calls Client.Login() to mint and
// persist a fresh token.
func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in and cache a bearer token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := runtimeFrom(cmd.Context())
			if rt.flags == nil {
				return errors.New("cli: login: no flags wired")
			}
			f := rt.flags

			// Username is required for any non-anonymous method.
			if f.Method != "anonymous" && f.User == "" {
				user, err := readLine(cmd, "Username: ")
				if err != nil {
					return fmt.Errorf("read username: %w", err)
				}
				if user == "" {
					return errors.New("cli: login: username is required")
				}
				f.User = user
			}

			// Password is required for local/ldap when no Token override is set.
			if f.Token == "" && f.Method != "anonymous" && f.Password == "" {
				password, err := promptPassword(cmd, rt)
				if err != nil {
					return fmt.Errorf("read password: %w", err)
				}
				f.Password = password
			}

			c, err := rt.buildClient()
			if err != nil {
				return err
			}
			if err := c.Login(cmd.Context()); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Logged in. Token cached.")
			return nil
		},
	}
}

// promptPassword reads a password from stdin without echoing. When rt has a
// passwordReader override (tests), use it. Otherwise, fall back to
// golang.org/x/term if stdin is a terminal, or a plain line-read for piped
// input.
func promptPassword(cmd *cobra.Command, rt *runtime) (string, error) {
	if rt.passwordReader != nil {
		return rt.passwordReader()
	}
	in := cmd.InOrStdin()
	// Try to use term-style hidden input when stdin is the real TTY.
	if file, ok := in.(*os.File); ok {
		fd := int(file.Fd())
		if term.IsTerminal(fd) {
			_, _ = fmt.Fprint(cmd.ErrOrStderr(), "Password: ")
			raw, err := term.ReadPassword(fd)
			_, _ = fmt.Fprintln(cmd.ErrOrStderr())
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(raw)), nil
		}
	}
	// Non-TTY: read a line. No echo control is possible; the caller chose to
	// pipe the password in.
	line, err := readLine(cmd, "Password: ")
	if err != nil {
		return "", err
	}
	return line, nil
}

// readLine prompts on stderr and reads one line from stdin (whitespace-trimmed).
func readLine(cmd *cobra.Command, prompt string) (string, error) {
	if prompt != "" {
		_, _ = fmt.Fprint(cmd.ErrOrStderr(), prompt)
	}
	rd := bufio.NewReader(cmd.InOrStdin())
	line, err := rd.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

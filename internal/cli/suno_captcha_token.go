// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const captchaTokenCommandTimeout = 30 * time.Second

type captchaTokenSourceFlags struct {
	tokenStdin    bool
	tokenCommand  string
	tokenProvider string
}

func addCaptchaTokenSourceFlags(cmd *cobra.Command, src *captchaTokenSourceFlags) {
	if cmd == nil || src == nil {
		return
	}
	cmd.Flags().BoolVar(&src.tokenStdin, "token-stdin", false, "Read hCaptcha token from stdin (avoids shell history)")
	cmd.Flags().StringVar(&src.tokenCommand, "token-command", "", "Run command and read hCaptcha token from stdout")
	cmd.Flags().StringVar(&src.tokenProvider, "token-provider", "", "Challenge token provider: auto, hcaptcha/1, or turnstile/2")
}

func resolveCaptchaToken(cmd *cobra.Command, explicit string, src captchaTokenSourceFlags) (string, error) {
	explicit = strings.TrimSpace(explicit)
	command := strings.TrimSpace(src.tokenCommand)

	sourceCount := 0
	if explicit != "" {
		sourceCount++
	}
	if src.tokenStdin {
		sourceCount++
	}
	if command != "" {
		sourceCount++
	}
	if sourceCount > 1 {
		return "", usageErr(fmt.Errorf("pass only one of --token, --token-stdin, or --token-command"))
	}

	switch {
	case explicit != "":
		return normalizeCaptchaToken("--token", explicit)
	case src.tokenStdin:
		in := io.Reader(os.Stdin)
		if cmd != nil {
			in = cmd.InOrStdin()
		}
		data, err := io.ReadAll(in)
		if err != nil {
			return "", usageErr(fmt.Errorf("reading --token-stdin: %w", err))
		}
		return normalizeCaptchaToken("--token-stdin", string(data))
	case command != "":
		return resolveCaptchaTokenCommand(cmd, command)
	default:
		return "", nil
	}
}

func resolveCaptchaTokenProvider(raw string, hasToken bool) (*int, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" || raw == "auto" {
		return nil, nil
	}
	if !hasToken {
		return nil, usageErr(fmt.Errorf("--token-provider requires --token, --token-stdin, or --token-command"))
	}
	var provider int
	switch raw {
	case "1", "hcaptcha", "h-captcha":
		provider = 1
	case "2", "turnstile", "cloudflare", "cloudflare-turnstile":
		provider = 2
	default:
		return nil, usageErr(fmt.Errorf("invalid --token-provider %q: use auto, hcaptcha/1, or turnstile/2", raw))
	}
	return &provider, nil
}

func normalizeCaptchaToken(source, raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", usageErr(fmt.Errorf("%s did not provide an hCaptcha token", source))
	}
	if strings.ContainsAny(token, "\r\n") {
		return "", usageErr(fmt.Errorf("%s produced multiple lines; output exactly one hCaptcha token", source))
	}
	return token, nil
}

func resolveCaptchaTokenCommand(cmd *cobra.Command, command string) (string, error) {
	ctx, cancel := context.WithTimeout(commandContext(cmd), captchaTokenCommandTimeout)
	defer cancel()

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	child := exec.CommandContext(ctx, shell, "-c", command)
	var stdout, stderr bytes.Buffer
	child.Stdout = &stdout
	child.Stderr = &stderr

	if err := child.Run(); err != nil {
		if ctx.Err() != nil {
			return "", usageErr(fmt.Errorf("--token-command timed out after %s", captchaTokenCommandTimeout))
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", usageErr(fmt.Errorf("running --token-command: %w: %s", err, detail))
		}
		return "", usageErr(fmt.Errorf("running --token-command: %w", err))
	}
	return normalizeCaptchaToken("--token-command", stdout.String())
}

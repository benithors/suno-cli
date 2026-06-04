// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveCaptchaTokenFromStdin(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetIn(strings.NewReader("  hc_test_token  \n"))

	got, err := resolveCaptchaToken(cmd, "", captchaTokenSourceFlags{tokenStdin: true})
	if err != nil {
		t.Fatalf("resolveCaptchaToken: %v", err)
	}
	if got != "hc_test_token" {
		t.Fatalf("token = %q, want hc_test_token", got)
	}
}

func TestResolveCaptchaTokenRejectsMultipleSources(t *testing.T) {
	_, err := resolveCaptchaToken(&cobra.Command{Use: "test"}, "hc_test_token", captchaTokenSourceFlags{tokenStdin: true})
	if err == nil {
		t.Fatal("resolveCaptchaToken with multiple sources succeeded, want usage error")
	}
	if !strings.Contains(err.Error(), "pass only one") {
		t.Fatalf("error = %q, want source conflict", err)
	}
	if ExitCode(err) != 2 {
		t.Fatalf("ExitCode = %d, want 2", ExitCode(err))
	}
}

func TestResolveCaptchaTokenCommand(t *testing.T) {
	got, err := resolveCaptchaToken(&cobra.Command{Use: "test"}, "", captchaTokenSourceFlags{tokenCommand: "printf 'hc_test_token\\n'"})
	if err != nil {
		t.Fatalf("resolveCaptchaToken: %v", err)
	}
	if got != "hc_test_token" {
		t.Fatalf("token = %q, want hc_test_token", got)
	}
}

func TestResolveCaptchaTokenCommandRejectsEmptyOutput(t *testing.T) {
	_, err := resolveCaptchaToken(&cobra.Command{Use: "test"}, "", captchaTokenSourceFlags{tokenCommand: "printf ''"})
	if err == nil {
		t.Fatal("empty --token-command output succeeded, want usage error")
	}
	if !strings.Contains(err.Error(), "did not provide") {
		t.Fatalf("error = %q, want empty token message", err)
	}
}

func TestResolveCaptchaTokenRejectsMultilineOutput(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetIn(strings.NewReader("hc_first\nhc_second\n"))

	_, err := resolveCaptchaToken(cmd, "", captchaTokenSourceFlags{tokenStdin: true})
	if err == nil {
		t.Fatal("multi-line --token-stdin input succeeded, want usage error")
	}
	if !strings.Contains(err.Error(), "multiple lines") {
		t.Fatalf("error = %q, want multi-line message", err)
	}
}

func TestResolveCaptchaTokenProvider(t *testing.T) {
	got, err := resolveCaptchaTokenProvider("turnstile", true)
	if err != nil {
		t.Fatalf("resolveCaptchaTokenProvider: %v", err)
	}
	if got == nil || *got != 2 {
		t.Fatalf("provider = %v, want 2", got)
	}

	got, err = resolveCaptchaTokenProvider("hcaptcha", true)
	if err != nil {
		t.Fatalf("resolveCaptchaTokenProvider hcaptcha: %v", err)
	}
	if got == nil || *got != 1 {
		t.Fatalf("provider = %v, want 1", got)
	}
}

func TestResolveCaptchaTokenProviderRequiresToken(t *testing.T) {
	_, err := resolveCaptchaTokenProvider("turnstile", false)
	if err == nil {
		t.Fatal("--token-provider without token succeeded, want usage error")
	}
	if !strings.Contains(err.Error(), "requires --token") {
		t.Fatalf("error = %q, want requires token message", err)
	}
}

func TestDescribeDryRunDoesNotRunTokenCommand(t *testing.T) {
	cmd := newSunoDescribeCmd(&rootFlags{dryRun: true})
	cmd.SetArgs([]string{"quick dry run", "--token-command", "exit 7"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run describe executed --token-command: %v", err)
	}
}

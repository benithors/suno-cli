// Copyright 2026 horknfbr. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestQueueGenerationOnCaptchaScrubsTokenAndListsSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body := buildGenerateBody(generateInput{
		createMode: "custom",
		mv:         sunoGenerateModels["v4"],
		title:      "Queue Song",
		tags:       "folk, cli",
		prompt:     "a recap of the Suno CLI fork and hCaptcha queue work",
		token:      "secret-hcaptcha-token",
	})
	cmd := &cobra.Command{Use: "create"}
	cmd.SetContext(context.Background())

	item, err := queueGenerationOnCaptcha(cmd, body, true, "./out", "workspace-1", captchaErr)
	if err != nil {
		t.Fatalf("queueGenerationOnCaptcha: %v", err)
	}
	if item.Body.Token != nil {
		t.Fatalf("returned item stored token %q, want nil", *item.Body.Token)
	}

	s, err := openDefaultStore(cmd.Context())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	raw, err := s.Get(generationQueueResource, item.ID)
	if err != nil {
		t.Fatalf("read queue item: %v", err)
	}
	if strings.Contains(string(raw), "secret-hcaptcha-token") {
		t.Fatalf("persisted queue item leaked captcha token: %s", string(raw))
	}

	var saved generationQueueItem
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("decode queue item: %v", err)
	}
	if saved.Status != generationQueueStatusBlocked {
		t.Errorf("status = %q, want %q", saved.Status, generationQueueStatusBlocked)
	}
	if saved.Title != "Queue Song" || saved.Tags != "folk, cli" {
		t.Errorf("summary fields not copied: title=%q tags=%q", saved.Title, saved.Tags)
	}
	if saved.Body.Token != nil {
		t.Fatalf("saved item token = %v, want nil", saved.Body.Token)
	}

	summaries, err := listGenerationQueueItems(cmd.Context(), s, generationQueueStatusBlocked, 10)
	if err != nil {
		t.Fatalf("list queue: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	if summaries[0].ID != item.ID || summaries[0].PromptPreview == "" {
		t.Errorf("summary = %+v, want queued item with prompt preview", summaries[0])
	}

	empty, err := listGenerationQueueItems(cmd.Context(), s, "missing_status", 10)
	if err != nil {
		t.Fatalf("list empty queue filter: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty filtered list = %#v, want non-nil empty slice", empty)
	}
}

func TestCaptchaGateEnvelopeIncludesQueuedItem(t *testing.T) {
	env := captchaGateEnvelope(&generationQueueItem{ID: "gq_test"})
	if env["queued"] != true {
		t.Errorf("queued = %v, want true", env["queued"])
	}
	if env["queue_id"] != "gq_test" {
		t.Errorf("queue_id = %v, want gq_test", env["queue_id"])
	}
	if !strings.Contains(env["retry_hint"].(string), "generate queue retry gq_test") {
		t.Errorf("retry_hint = %v", env["retry_hint"])
	}
}

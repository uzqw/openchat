// Best-effort reply-done notification: after a successful ask, push a
// short ntfy alert to the configured publish URL (title = first 10 runes
// of the prompt, message = first 15 runes of the answer). The configured
// URL is the full publish address a deployment owns
// (https://ntfy.sh/<topic>); ntfy only parses the JSON fields when they
// are posted to the server root with the topic in the body — a JSON body
// sent to a topic-path URL is delivered as one plain-text blob — so the
// URL is split back into server root + topic before posting. Fire in a
// goroutine with its own timeout so a slow or unreachable ntfy host never
// delays the queue worker — a failed push is logged and ignored, the
// answer is already stored.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// notifyTimeout is the kill ceiling for one push request.
const notifyTimeout = 10 * time.Second

// truncateRunes keeps the first n runes of s (byte slicing would cut
// multi-byte text mid-character).
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ntfyPayload builds the JSON body handed to ntfy: topic, title, message
// and the speech-bubble tag so the push renders as a comment.
func ntfyPayload(topic, title, message string) ([]byte, error) {
	// opencli's confirmed display wrapper; the frontend strips it too
	message = strings.TrimPrefix(message, "💬 ")
	return json.Marshal(map[string]any{
		"topic":   topic,
		"title":   truncateRunes(title, 10),
		"message": truncateRunes(message, 15),
		"tags":    []string{"speech_balloon"},
	})
}

// splitPublishURL splits a full publish address (https://ntfy.sh/topic)
// into the server root URL (https://ntfy.sh/) and the topic path.
func splitPublishURL(publishURL string) (root, topic string, err error) {
	u, err := url.Parse(publishURL)
	if err != nil {
		return "", "", err
	}
	if u.Scheme == "" || u.Host == "" || u.Path == "" || u.Path == "/" {
		return "", "", fmt.Errorf("publish URL needs a server and a topic path (https://host/topic)")
	}
	root = u.Scheme + "://" + u.Host + "/"
	topic = strings.TrimPrefix(u.Path, "/")
	return root, topic, nil
}

// NotifyReply pushes the completion alert to the publish URL ("" no-ops
// via the caller). Never returns an error: the caller must not fail the
// task on a notification problem.
func NotifyReply(publishURL, prompt, answer string) {
	root, topic, err := splitPublishURL(publishURL)
	if err != nil {
		log.Printf("ntfy: invalid publish URL %q: %v", publishURL, err)
		return
	}
	body, err := ntfyPayload(topic, prompt, answer)
	if err != nil {
		log.Printf("ntfy: build payload: %v", err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, root, bytes.NewReader(body))
		if err != nil {
			log.Printf("ntfy: new request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("ntfy: push failed: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

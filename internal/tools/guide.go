package tools

import (
	"context"
	"embed"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed guides/*.md
var guideFiles embed.FS

// guideTopics maps a topic to its guide file, in the order they are listed.
var guideTopics = []struct{ topic, file, about string }{
	{"how_replies_work", "how-replies-work.md", "every step from a visitor message to a reply, and the tool that controls each"},
	{"product_analysis", "product-analysis.md", "how to study a client's product and map it to persona, knowledge, integrations and policies"},
	{"knowledge", "knowledge.md", "reference vs instruction documents, FAQs, crawling, testing"},
	{"integrations", "integrations.md", "connecting an API, request bindings, identity/secret/metadata placeholders"},
	{"policies", "policies.md", "action policies: required steps, verified identity, argument rules"},
	{"security", "security.md", "what API keys can and can't do"},
}

type guideInput struct {
	Topic string `json:"topic,omitempty" jsonschema:"how_replies_work, product_analysis, knowledge, integrations, policies or security; omit to list topics"`
}

func registerGuide(r *registry) {
	add(r, read, nil, &mcp.Tool{
		Name: "get_erghi_guide",
		Description: "Erghi's own guide to how the assistant works and how to configure it well. Read how_replies_work before changing anything that affects chat, " +
			"and integrations or policies before connecting an API.",
	}, func(_ context.Context, in guideInput) (any, error) {
		return guide(in.Topic)
	})
}

func guide(topic string) (string, error) {
	topic = strings.TrimSpace(strings.ToLower(topic))
	i := slices.IndexFunc(guideTopics, func(g struct{ topic, file, about string }) bool { return g.topic == topic })
	if i < 0 {
		var b strings.Builder
		if topic != "" {
			fmt.Fprintf(&b, "No guide %q. ", topic)
		}
		b.WriteString("Topics:\n")
		for _, g := range guideTopics {
			fmt.Fprintf(&b, "- %s: %s\n", g.topic, g.about)
		}
		return b.String(), nil
	}
	data, err := guideFiles.ReadFile("guides/" + guideTopics[i].file)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

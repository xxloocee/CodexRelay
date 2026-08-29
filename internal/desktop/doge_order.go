package desktop

import (
	"strconv"
	"strings"

	"codexrelay/internal/config"
)

func dogeTokenOrderKey(token config.DogeToken) string {
	if token.ID > 0 {
		return strconv.FormatInt(token.ID, 10)
	}
	return ""
}

func mergeDogeTokenOrder(previous []string, tokens []config.DogeToken) []string {
	known := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if key := dogeTokenOrderKey(token); key != "" {
			known[key] = struct{}{}
		}
	}
	result := make([]string, 0, len(known))
	seen := make(map[string]struct{}, len(known))
	for _, key := range previous {
		key = strings.TrimSpace(key)
		if _, ok := known[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	for _, token := range tokens {
		key := dogeTokenOrderKey(token)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func orderDogeTokens(order []string, tokens []config.DogeToken) []config.DogeToken {
	byKey := make(map[string]config.DogeToken, len(tokens))
	for _, token := range tokens {
		if key := dogeTokenOrderKey(token); key != "" {
			byKey[key] = token
		}
	}
	ordered := make([]config.DogeToken, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, key := range order {
		if token, ok := byKey[key]; ok {
			ordered = append(ordered, token)
			seen[key] = struct{}{}
		}
	}
	for _, token := range tokens {
		key := dogeTokenOrderKey(token)
		if key == "" {
			ordered = append(ordered, token)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ordered = append(ordered, token)
	}
	return ordered
}

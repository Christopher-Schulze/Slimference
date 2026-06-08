# T335 OpenAI Prompt-Cache Negative-Net Guard

## Why

T334 made provider prompt-cache read/create/net accounting visible. The product also needs an automatic guard that prevents optional OpenAI prompt-cache hints from staying active when real provider usage shows they repeatedly create more cache-write cost than cache-read credit.

## Acceptance

- OpenAI prompt-cache steering observes provider `cached_tokens` and cache-creation tokens after upstream usage is parsed.
- Negative cache net is tracked per generated prompt-cache key, not globally by provider or model.
- A key enters cooldown only after repeated negative samples and a minimum token-loss threshold.
- The first create-only warmup request does not disable the key by itself.
- Cooldown suppresses only the generated cache key; other keys keep working.
- Suppression removes optional cache hints only and never changes model-facing prompt content.
- CodexChatGPT routes remain untouched by OpenAI prompt-cache fields.
- Tests cover negative-net cooldown, TTL expiry, and existing rejection cooldown behavior.

## Notes

- Guard scope: generic OpenAI API prompt-cache steering only.
- Trigger: 3 negative samples and at least 1024 net-lost provider cache tokens.
- Cooldown TTL: 30 minutes.
- Product drawdown: none expected; the guard only stops adding optional provider cache metadata after measured negative economics.

## Verification

- `go test ./internal/proxy -run 'TestOpenAIPromptCacheNegativeNetCooldown|TestInjectOpenAIPromptCache|TestOpenAIPromptCacheRejectedCooldown' -count=1`

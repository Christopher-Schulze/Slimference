package types

import "testing"

func TestCapabilitiesFor_AnthropicDefaults(t *testing.T) {
	caps := CapabilitiesFor(Anthropic)
	if !caps.SupportsTemperatureZero || !caps.SupportsCachedPrefix {
		t.Fatalf("anthropic defaults wrong: %+v", caps)
	}
	if caps.SupportsSeed || caps.SupportsResponseID {
		t.Fatalf("anthropic must not claim seed / response_id support")
	}
}

func TestCapabilitiesFor_OpenAIDefaults(t *testing.T) {
	caps := CapabilitiesFor(OpenAI)
	if !caps.SupportsSeed || !caps.SupportsResponseID {
		t.Fatalf("openai defaults wrong: %+v", caps)
	}
}

func TestCapabilitiesFor_CodexDefaults(t *testing.T) {
	caps := CapabilitiesFor(CodexChatGPT)
	if !caps.SupportsResponseID || !caps.SupportsTemperatureZero {
		t.Fatalf("codex defaults wrong: %+v", caps)
	}
}

func TestCapabilitiesFor_Unknown(t *testing.T) {
	caps := CapabilitiesFor(Provider(999))
	if caps.SupportsSeed || caps.SupportsCachedPrefix {
		t.Fatalf("unknown provider must return zero-value caps: %+v", caps)
	}
}

func TestSetProviderCapabilities_RoundTrip(t *testing.T) {
	custom := ProviderCapabilities{SupportsSeed: true}
	restore := SetProviderCapabilities(Anthropic, custom)
	if !CapabilitiesFor(Anthropic).SupportsSeed {
		t.Fatal("override did not take effect")
	}
	restore()
	if CapabilitiesFor(Anthropic).SupportsSeed {
		t.Fatal("restore did not revert")
	}
}

func TestSetProviderCapabilities_NewProvider(t *testing.T) {
	newProv := Provider(42)
	caps := ProviderCapabilities{SupportsLogprobs: true}
	restore := SetProviderCapabilities(newProv, caps)
	if !CapabilitiesFor(newProv).SupportsLogprobs {
		t.Fatal("new provider override did not take effect")
	}
	restore()
	if CapabilitiesFor(newProv).SupportsLogprobs {
		t.Fatal("new provider restore did not delete entry")
	}
}

func TestTrustClass_UpstreamProviders(t *testing.T) {
	for _, p := range []Provider{Anthropic, OpenAI, CodexChatGPT} {
		caps := CapabilitiesFor(p)
		if caps.TrustClass != TrustClassUpstreamProvider {
			t.Errorf("%s.TrustClass = %q, want %q", p, caps.TrustClass, TrustClassUpstreamProvider)
		}
	}
}

func TestTrustClass_MiniMaxExternal(t *testing.T) {
	caps := CapabilitiesFor(MiniMax)
	if caps.TrustClass != TrustClassExternalThirdParty {
		t.Errorf("MiniMax.TrustClass = %q, want %q", caps.TrustClass, TrustClassExternalThirdParty)
	}
}

func TestEffectiveTrustClass_Default(t *testing.T) {
	got := EffectiveTrustClass(MiniMax, "")
	if got != TrustClassExternalThirdParty {
		t.Errorf("EffectiveTrustClass(minimax, empty) = %q, want %q", got, TrustClassExternalThirdParty)
	}
}

func TestEffectiveTrustClass_OverrideUpstream(t *testing.T) {
	got := EffectiveTrustClass(MiniMax, TrustClassUpstreamProvider)
	if got != TrustClassUpstreamProvider {
		t.Errorf("EffectiveTrustClass(minimax, upstream) = %q, want %q", got, TrustClassUpstreamProvider)
	}
}

func TestEffectiveTrustClass_OverrideInvalid(t *testing.T) {
	got := EffectiveTrustClass(MiniMax, "nonsense")
	if got != TrustClassExternalThirdParty {
		t.Errorf("EffectiveTrustClass(minimax, nonsense) = %q, want external fallback", got)
	}
}

func TestEffectiveTrustClass_UnknownProvider(t *testing.T) {
	got := EffectiveTrustClass(Provider(999), "")
	if got != TrustClassUnknown {
		t.Errorf("EffectiveTrustClass(unknown, empty) = %q, want %q", got, TrustClassUnknown)
	}
}

func TestMiniMaxProviderString(t *testing.T) {
	if MiniMax.String() != "minimax" {
		t.Errorf("MiniMax.String() = %q, want minimax", MiniMax.String())
	}
}
